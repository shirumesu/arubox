package fileindex

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"

	"github.com/shirumesu/arubox/internal/logging"
)

const (
	serviceName = "aruboxindex"
	pipeName    = `\\.\pipe\arubox-fileindex`
	// frameHello is the program's opening frame.
	frameHello byte = 0x10
)

// The service needs administrator rights only to read volumes. It keeps no
// index of its own: it lists each volume's file table and streams its
// changes to the program, which may be a program of only the user who
// installed the service, through a pipe only that user can open.

type service struct {
	sid string // the user the pipe is open to
	log *slog.Logger
}

// runService is what the service manager starts.
func runService(sid string) error {
	dir := filepath.Join(os.Getenv("ProgramData"), "arubox", "logs")
	out, closeLog, err := logging.Open(dir)
	if err != nil {
		return err
	}
	defer closeLog()
	s := &service{sid: sid, log: logging.New(out, slog.LevelInfo).With("process", "index service")}
	s.log.Info("starting")
	return svc.Run(serviceName, s)
}

// Execute runs until the service manager stops the service.
func (s *service) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		s.log.Error("create stop event", "error", err)
		return true, 1
	}
	failed := make(chan error, 1)
	go func() { failed <- s.listen(stop) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-failed:
			s.log.Error("listen", "error", err)
			status <- svc.Status{State: svc.StopPending}
			return true, 1
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				_ = windows.SetEvent(stop)
				s.log.Info("stopping")
				return false, 0
			}
		}
	}
}

// listen accepts the program's connections until stop is set.
func (s *service) listen(stop windows.Handle) error {
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;SY)(A;;GRGW;;;" + s.sid + ")")
	if err != nil {
		return fmt.Errorf("pipe security: %w", err)
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return err
	}
	// The first instance claims the name, so no other program can have
	// created the pipe before the service and pose as it.
	flags := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED | windows.FILE_FLAG_FIRST_PIPE_INSTANCE)
	for {
		h, err := windows.CreateNamedPipe(name, flags,
			windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
			windows.PIPE_UNLIMITED_INSTANCES, 1<<20, 1<<16, 0, sa)
		if err != nil {
			return fmt.Errorf("create pipe: %w", err)
		}
		flags &^= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
		p := &handle{h: h, stops: []windows.Handle{stop}}
		_, err = p.do(func(ov *windows.Overlapped) error { return windows.ConnectNamedPipe(h, ov) })
		switch {
		case err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED):
			go s.serve(h, stop)
		case errors.Is(err, errStopped):
			closeHandle(h)
			return nil
		default:
			s.log.Warn("accept connection", "error", err)
			closeHandle(h)
		}
	}
}

// volumeInfo is a volume the service indexes.
type volumeInfo struct {
	guid, mount string
}

// errClientGone wraps a failure to send to the program.
var errClientGone = errors.New("program disconnected")

// serve runs one connection: it answers the hello, then indexes and
// follows every volume until the program disconnects.
func (s *service) serve(h windows.Handle, serviceStop windows.Handle) {
	defer closeHandle(h)
	sessionStop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		s.log.Error("create session event", "error", err)
		return
	}
	defer closeHandle(sessionStop)
	stops := []windows.Handle{sessionStop, serviceStop}
	conn := &handle{h: h, stops: stops}
	r := bufio.NewReader(conn)
	kind, payload, err := readFrame(r, nil)
	if err != nil || kind != frameHello {
		return
	}
	version, states, err := decodeHello(payload)
	if err != nil {
		return
	}
	vols := ntfsVolumes()
	guids := make([]string, len(vols))
	for i, v := range vols {
		guids[i] = v.guid
	}
	if writeFrame(conn, frameHello, encodeWelcome(guids)) != nil || version != protocolVersion {
		_ = windows.DisconnectNamedPipe(h)
		return
	}
	s.log.Info("program connected", "volumes", len(vols))

	var mu sync.Mutex
	send := func(kind byte, payload []byte) error {
		mu.Lock()
		defer mu.Unlock()
		if err := writeFrame(conn, kind, payload); err != nil {
			return fmt.Errorf("%w: %w", errClientGone, err)
		}
		return nil
	}
	saved := map[string]volumeState{}
	for _, st := range states {
		saved[st.GUID] = st
	}
	var wg sync.WaitGroup
	for i, v := range vols {
		st, ok := saved[v.guid]
		wg.Go(func() { s.runVolume(byte(i), v, st, ok, send, stops) })
	}
	// The program sends nothing more, so a read returns only when it
	// disconnects.
	_, _ = conn.Read(make([]byte, 1))
	_ = windows.SetEvent(sessionStop)
	wg.Wait()
	_ = windows.DisconnectNamedPipe(h)
	s.log.Info("program disconnected")
}

// runVolume indexes a volume, and indexes it again from scratch whenever
// that fails, until the connection ends.
func (s *service) runVolume(idx byte, info volumeInfo, saved volumeState, resume bool, send func(byte, []byte) error, stops []windows.Handle) {
	for {
		started := time.Now()
		err := s.indexVolume(idx, info, saved, resume, send, stops)
		if errors.Is(err, errStopped) || errors.Is(err, errClientGone) {
			return
		}
		s.log.Warn("volume indexing failed", "volume", info.mount, "error", err)
		var e encoder
		e.u8(idx)
		e.str(err.Error())
		if send(frameReset, e.b) != nil {
			return
		}
		resume = false
		// A journal that wrapped is listed again at once; anything else,
		// or a journal lost again right after a listing, waits a minute.
		if !errors.Is(err, errJournalLost) || time.Since(started) < time.Minute {
			if waitStop(stops, 60_000) {
				return
			}
		}
	}
}

// indexVolume lists a volume, unless the program's saved index can catch
// up from its journal, then follows the journal.
func (s *service) indexVolume(idx byte, info volumeInfo, saved volumeState, resume bool, send func(byte, []byte) error, stops []windows.Handle) error {
	v, err := openNTFS(info.guid, stops)
	if err != nil {
		return err
	}
	defer v.close()
	j, err := v.queryJournal()
	if err != nil {
		return err
	}
	full := !resume || saved.JournalID != j.id || saved.NextUSN < j.first || saved.NextUSN > j.next
	var e encoder
	e.u8(idx)
	e.str(info.guid)
	e.str(info.mount)
	e.u64(j.id)
	e.u8(boolByte(full))
	if err := send(frameVolume, e.b); err != nil {
		return err
	}
	start := saved.NextUSN
	if full {
		// Changes made while listing are in the journal after this
		// point, and are replayed once the listing is done.
		start = j.next
		began := time.Now()
		extents, err := v.mftExtents()
		if err != nil {
			return err
		}
		if err := v.scan(idx, extents, func(b []byte) error { return send(frameRecords, b) }); err != nil {
			return err
		}
		var e encoder
		e.u8(idx)
		e.u64(uint64(start))
		if err := send(frameScanned, e.b); err != nil {
			return err
		}
		s.log.Info("volume listed", "volume", info.mount, "took", time.Since(began).Round(time.Millisecond))
		// The service keeps nothing of a listing; return its buffers.
		debug.FreeOSMemory()
	}
	return v.follow(idx, j.id, start, send)
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// ntfsVolumes returns the fixed NTFS volumes that are mounted somewhere.
func ntfsVolumes() []volumeInfo {
	buf := make([]uint16, windows.MAX_PATH+1)
	h, err := windows.FindFirstVolume(&buf[0], uint32(len(buf)))
	if err != nil {
		return nil
	}
	defer windows.FindVolumeClose(h)
	var out []volumeInfo
	for {
		guid := windows.UTF16ToString(buf)
		if mount := volumeMount(guid); mount != "" && fixedNTFS(mount) {
			out = append(out, volumeInfo{guid: guid, mount: mount})
		}
		if windows.FindNextVolume(h, &buf[0], uint32(len(buf))) != nil {
			return out
		}
	}
}

// volumeMount returns where a volume is mounted, preferring a drive
// letter, or "" when it is not mounted.
func volumeMount(guid string) string {
	g, err := windows.UTF16PtrFromString(guid)
	if err != nil {
		return ""
	}
	names := make([]uint16, 1024)
	var n uint32
	if windows.GetVolumePathNamesForVolumeName(g, &names[0], uint32(len(names)), &n) != nil {
		return ""
	}
	best := ""
	for start := 0; start < len(names) && names[start] != 0; {
		end := start
		for end < len(names) && names[end] != 0 {
			end++
		}
		p := windows.UTF16ToString(names[start:end])
		if best == "" || (len(p) == 3 && len(best) != 3) {
			best = p
		}
		start = end + 1
	}
	return best
}

func fixedNTFS(mount string) bool {
	m, err := windows.UTF16PtrFromString(mount)
	if err != nil || windows.GetDriveType(m) != windows.DRIVE_FIXED {
		return false
	}
	fs := make([]uint16, 32)
	if windows.GetVolumeInformation(m, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))) != nil {
		return false
	}
	return windows.UTF16ToString(fs) == "NTFS"
}
