package fileindex

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	fsctlGetNTFSVolumeData = 0x00090064
	fsctlGetNTFSFileRecord = 0x00090068
	fsctlQueryUSNJournal   = 0x000900f4
	fsctlReadUSNJournal    = 0x000900bb
	fsctlCreateUSNJournal  = 0x000900e7

	errJournalDeleteInProgress = windows.Errno(1178)
	errJournalNotActive        = windows.Errno(1179)
	errJournalEntryDeleted     = windows.Errno(1181)
)

// errStopped is returned by I/O given up because its stop event was set.
var errStopped = errors.New("fileindex: stopped")

// handle does overlapped I/O on a file, a volume or a pipe, and gives up
// when any of its stop events is set. Each call has its own event, so a
// handle can read and write from different goroutines at once.
type handle struct {
	h     windows.Handle
	stops []windows.Handle
}

func (f *handle) do(start func(ov *windows.Overlapped) error) (uint32, error) {
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer closeHandle(ev)
	ov := &windows.Overlapped{HEvent: ev}
	if err := start(ov); err != nil && !errors.Is(err, windows.ERROR_IO_PENDING) {
		return 0, err
	}
	which, err := windows.WaitForMultipleObjects(append([]windows.Handle{ev}, f.stops...), false, windows.INFINITE)
	if err != nil {
		return 0, err
	}
	var n uint32
	if which != windows.WAIT_OBJECT_0 {
		// Wait for the cancelled I/O, which still owns ov and the buffer.
		_ = windows.CancelIoEx(f.h, ov)
		_ = windows.GetOverlappedResult(f.h, ov, &n, true)
		return 0, errStopped
	}
	err = windows.GetOverlappedResult(f.h, ov, &n, false)
	return n, err
}

func (f *handle) Read(p []byte) (int, error) {
	n, err := f.do(func(ov *windows.Overlapped) error { return windows.ReadFile(f.h, p, nil, ov) })
	return int(n), err
}

func (f *handle) Write(p []byte) (int, error) {
	n, err := f.do(func(ov *windows.Overlapped) error { return windows.WriteFile(f.h, p, nil, ov) })
	return int(n), err
}

func (f *handle) readAt(off int64, p []byte) error {
	n, err := f.do(func(ov *windows.Overlapped) error {
		ov.Offset, ov.OffsetHigh = uint32(off), uint32(off>>32)
		return windows.ReadFile(f.h, p, nil, ov)
	})
	if err == nil && int(n) != len(p) {
		err = fmt.Errorf("short read at %d", off)
	}
	return err
}

func (f *handle) ioctl(code uint32, in, out []byte) (int, error) {
	n, err := f.do(func(ov *windows.Overlapped) error {
		return windows.DeviceIoControl(f.h, code, first(in), uint32(len(in)), first(out), uint32(len(out)), nil, ov)
	})
	return int(n), err
}

func first(b []byte) *byte {
	if len(b) == 0 {
		return nil
	}
	return &b[0]
}

// waitStop waits for ms milliseconds and reports whether a stop event was
// set meanwhile.
func waitStop(stops []windows.Handle, ms uint32) bool {
	which, _ := windows.WaitForMultipleObjects(stops, false, ms)
	return which != uint32(windows.WAIT_TIMEOUT)
}

// ntfsVolume is an NTFS volume opened for reading its file table.
type ntfsVolume struct {
	handle
	recordSize  int
	sectorSize  int64 // reads of a volume must be whole sectors
	clusterSize int64
	mftStart    int64 // the byte offset of the file table's first record
	mftValid    int64 // how many bytes of the file table are in use
}

func openNTFS(guid string, stops []windows.Handle) (*ntfsVolume, error) {
	name, err := windows.UTF16PtrFromString(strings.TrimSuffix(guid, `\`))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("open volume: %w", err)
	}
	v := &ntfsVolume{handle: handle{h: h, stops: stops}}
	// NTFS_VOLUME_DATA_BUFFER
	out := make([]byte, 96)
	if _, err := v.ioctl(fsctlGetNTFSVolumeData, nil, out); err != nil {
		v.close()
		return nil, fmt.Errorf("read volume data: %w", err)
	}
	le := binary.LittleEndian
	v.sectorSize = int64(le.Uint32(out[40:]))
	v.clusterSize = int64(le.Uint32(out[44:]))
	v.recordSize = int(le.Uint32(out[48:]))
	v.mftValid = int64(le.Uint64(out[56:]))
	v.mftStart = int64(le.Uint64(out[64:])) * v.clusterSize
	if v.recordSize < 512 || v.sectorSize == 0 || v.clusterSize == 0 {
		v.close()
		return nil, errors.New("unexpected NTFS volume data")
	}
	return v, nil
}

func (v *ntfsVolume) close() { closeHandle(v.h) }

func closeHandle(h windows.Handle) { _ = windows.CloseHandle(h) }

// Attribute types.
const (
	attrStandardInfo = 0x10
	attrList         = 0x20
	attrFileName     = 0x30
	attrData         = 0x80
	attrEnd          = 0xffffffff
)

// Flags in a file name attribute and a record header.
const (
	fnDirectory      = 0x10000000
	recordInUse      = 0x0001
	recordIsDir      = 0x0002
	fileHidden       = 0x2
	fileSystem       = 0x4
	namespaceDOS     = 2
	firstUserRecord  = 24 // records below are the file system's own
	maxAttrListBytes = 1 << 20
)

type fileName struct {
	parent    uint64
	name      string
	flags     uint32
	namespace byte
}

// dataRun is one piece of an unnamed non-resident $DATA attribute: the
// first cluster of the file it covers and its run list.
type dataRun struct {
	startVCN int64
	runs     []byte
}

// fileRecord is what the index needs of an MFT record.
type fileRecord struct {
	inUse, dir bool
	seq        uint16
	base       uint64 // the base record of an extension record, or 0
	attrs      uint32 // from $STANDARD_INFORMATION
	hasStdInfo bool
	names      []fileName
	attrList   []byte // a resident attribute list
	data       []dataRun
}

// applyFixups undoes the update sequence of a record read from disk: the
// last two bytes of every 512-byte stride were swapped for a check value
// when it was written. It reports false when a stride does not carry the
// check value, which means the record was torn, or its update sequence was
// already undone, as in records the file system hands out itself.
func applyFixups(b []byte) bool {
	le := binary.LittleEndian
	usaOff, usaCount := int(le.Uint16(b[4:])), int(le.Uint16(b[6:]))
	if usaCount < 2 || usaOff+2*usaCount > len(b) || len(b)%(usaCount-1) != 0 {
		return false
	}
	stride := len(b) / (usaCount - 1)
	check := b[usaOff : usaOff+2]
	for i := 1; i < usaCount; i++ {
		end := i*stride - 2
		if b[end] != check[0] || b[end+1] != check[1] {
			return false
		}
	}
	for i := 1; i < usaCount; i++ {
		end := i*stride - 2
		copy(b[end:end+2], b[usaOff+2*i:usaOff+2*i+2])
	}
	return true
}

// parseRecord reads a record whose update sequence is undone. withData
// also collects the $DATA run lists, which only the file table's own
// record needs.
func parseRecord(b []byte, withData bool) (fileRecord, bool) {
	le := binary.LittleEndian
	if len(b) < 48 || string(b[:4]) != "FILE" {
		return fileRecord{}, false
	}
	flags := le.Uint16(b[0x16:])
	r := fileRecord{
		inUse: flags&recordInUse != 0,
		dir:   flags&recordIsDir != 0,
		seq:   le.Uint16(b[0x10:]),
		base:  le.Uint64(b[0x20:]),
	}
	used := min(int(le.Uint32(b[0x18:])), len(b))
	for off := int(le.Uint16(b[0x14:])); off+16 <= used; {
		typ := le.Uint32(b[off:])
		length := int(le.Uint32(b[off+4:]))
		if typ == attrEnd || length < 16 || off+length > used {
			break
		}
		a := b[off : off+length]
		nonResident := a[8] != 0
		named := a[9] != 0
		var value []byte
		if !nonResident && len(a) >= 0x18 {
			vlen, voff := int(le.Uint32(a[0x10:])), int(le.Uint16(a[0x14:]))
			if voff+vlen <= len(a) {
				value = a[voff : voff+vlen]
			}
		}
		switch {
		case typ == attrStandardInfo && len(value) >= 0x24:
			r.attrs, r.hasStdInfo = le.Uint32(value[0x20:]), true
		case typ == attrFileName && len(value) >= 0x42:
			n := int(value[0x40])
			if 0x42+2*n <= len(value) {
				u := make([]uint16, n)
				for i := range u {
					u[i] = le.Uint16(value[0x42+2*i:])
				}
				r.names = append(r.names, fileName{
					parent:    le.Uint64(value),
					name:      string(utf16.Decode(u)),
					flags:     le.Uint32(value[0x38:]),
					namespace: value[0x41],
				})
			}
		case typ == attrList && value != nil:
			r.attrList = value
		case withData && typ == attrData && nonResident && !named && len(a) >= 0x40:
			runOff := int(le.Uint16(a[0x20:]))
			if runOff < len(a) {
				r.data = append(r.data, dataRun{startVCN: int64(le.Uint64(a[0x10:])), runs: a[runOff:]})
			}
		}
		off += length
	}
	return r, true
}

// attrListRefs returns the records an attribute list says hold attributes
// of type typ (and, for $DATA, the unnamed stream).
func attrListRefs(list []byte, typ uint32) []uint64 {
	le := binary.LittleEndian
	var refs []uint64
	for off := 0; off+0x1a <= len(list); {
		length := int(le.Uint16(list[off+4:]))
		if length < 0x1a {
			break
		}
		if le.Uint32(list[off:]) == typ && (typ != attrData || list[off+6] == 0) {
			ref := le.Uint64(list[off+0x10:])
			if !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
		}
		off += length
	}
	return refs
}

// extent is a run of clusters of the file table, in order.
type extent struct {
	lcn      int64 // -1 for a sparse run
	clusters int64
}

func decodeRuns(runs []byte) []extent {
	var out []extent
	var lcn int64
	for i := 0; i < len(runs) && runs[i] != 0; {
		lenSize, offSize := int(runs[i]&0x0f), int(runs[i]>>4)
		i++
		if lenSize == 0 || lenSize > 8 || offSize > 8 || i+lenSize+offSize > len(runs) {
			break
		}
		var length int64
		for k := lenSize - 1; k >= 0; k-- {
			length = length<<8 | int64(runs[i+k])
		}
		i += lenSize
		if offSize == 0 {
			out = append(out, extent{lcn: -1, clusters: length})
			continue
		}
		delta := int64(int8(runs[i+offSize-1]))
		for k := offSize - 2; k >= 0; k-- {
			delta = delta<<8 | int64(runs[i+k])
		}
		i += offSize
		lcn += delta
		out = append(out, extent{lcn: lcn, clusters: length})
	}
	return out
}

// mftExtents returns where the file table lies on disk. A fragmented
// table keeps part of its run list in extension records named by its
// attribute list.
func (v *ntfsVolume) mftExtents() ([]extent, error) {
	b := make([]byte, roundUp(int64(v.recordSize), v.sectorSize))
	if err := v.readAt(v.mftStart, b); err != nil {
		return nil, fmt.Errorf("read file table record: %w", err)
	}
	b = b[:v.recordSize]
	if !applyFixups(b) {
		return nil, errors.New("file table record is damaged")
	}
	r, ok := parseRecord(b, true)
	if !ok {
		return nil, errors.New("file table record is damaged")
	}
	runs := r.data
	for _, ref := range attrListRefs(r.attrList, attrData) {
		if recordNumber(ref) == 0 {
			continue
		}
		eb, found, err := v.fileRecord(ref)
		if err != nil {
			return nil, err
		}
		if found {
			if er, ok := parseRecord(eb, true); ok {
				runs = append(runs, er.data...)
			}
		}
	}
	slices.SortFunc(runs, func(a, b dataRun) int { return int(a.startVCN - b.startVCN) })
	runs = slices.CompactFunc(runs, func(a, b dataRun) bool { return a.startVCN == b.startVCN })
	var out []extent
	for _, d := range runs {
		out = append(out, decodeRuns(d.runs)...)
	}
	if len(out) == 0 {
		return nil, errors.New("file table has no extents")
	}
	return out, nil
}

// fileRecord asks the file system for a record by number. found is false
// when the record is not in use.
func (v *ntfsVolume) fileRecord(frn uint64) (rec []byte, found bool, err error) {
	in := binary.LittleEndian.AppendUint64(nil, uint64(recordNumber(frn)))
	out := make([]byte, 12+v.recordSize)
	if _, err := v.ioctl(fsctlGetNTFSFileRecord, in, out); err != nil {
		return nil, false, err
	}
	// The file system answers with the nearest record in use at or below
	// the one asked for.
	if recordNumber(binary.LittleEndian.Uint64(out)) != recordNumber(frn) {
		return nil, false, nil
	}
	n := min(int(binary.LittleEndian.Uint32(out[8:])), v.recordSize)
	rec = out[12 : 12+n]
	applyFixups(rec)
	return rec, true, nil
}

// entryFlags are the index flags of a base record.
func (r *fileRecord) entryFlags() byte {
	var f byte
	if r.dir {
		f |= flagDir
	}
	if r.attrs&fileHidden != 0 {
		f |= flagHidden
	}
	if r.attrs&fileSystem != 0 {
		f |= flagSystem
	}
	return f
}

// nameFlags are the index flags a file name attribute records, for names
// found in an extension record away from their base record.
func nameFlags(n fileName) byte {
	var f byte
	if n.flags&fnDirectory != 0 {
		f |= flagDir
	}
	if n.flags&fileHidden != 0 {
		f |= flagHidden
	}
	if n.flags&fileSystem != 0 {
		f |= flagSystem
	}
	return f
}

// linksOf returns a record's names, leaving out the DOS short names.
func linksOf(names []fileName) []link {
	var out []link
	for _, n := range names {
		l := link{parent: recordNumber(n.parent), name: n.name}
		if n.namespace != namespaceDOS && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// current reads a file's names as they are now. alive is false when the
// record was deleted or now holds another file.
func (v *ntfsVolume) current(frn uint64) (flags byte, links []link, alive bool, err error) {
	b, found, err := v.fileRecord(frn)
	if err != nil || !found {
		return 0, nil, false, err
	}
	r, ok := parseRecord(b, false)
	if !ok || !r.inUse || r.base != 0 || r.seq != sequence(frn) {
		return 0, nil, false, nil
	}
	names := r.names
	for _, ref := range attrListRefs(r.attrList, attrFileName) {
		if recordNumber(ref) == recordNumber(frn) {
			continue
		}
		eb, found, err := v.fileRecord(ref)
		if err != nil {
			return 0, nil, false, err
		}
		if found {
			if er, ok := parseRecord(eb, false); ok {
				names = append(names, er.names...)
			}
		}
	}
	return r.entryFlags(), linksOf(names), true, nil
}

func roundUp(n, to int64) int64 { return (n + to - 1) / to * to }

// scanChunk is a block of whole records read from the file table.
type scanChunk struct {
	buf      []byte
	firstRec int64
}

const scanChunkSize = 1 << 20

// scan reads the whole file table and passes the names in it to emit, in
// batches encoded as a frameRecords payload behind the volume index. It
// reads on one goroutine and parses on several; emit is called from those.
func (v *ntfsVolume) scan(idx byte, extents []extent, emit func([]byte) error) error {
	workers := min(4, runtime.NumCPU())
	free := make(chan []byte, workers*2)
	for range cap(free) {
		free <- make([]byte, scanChunkSize+v.recordSize+int(v.sectorSize))
	}
	chunks := make(chan scanChunk, workers)
	var (
		once     sync.Once
		firstErr error
		failed   = make(chan struct{})
	)
	fail := func(err error) {
		once.Do(func() { firstErr = err; close(failed) })
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for c := range chunks {
				if err := v.parseChunk(idx, c, emit); err != nil {
					fail(err)
				}
				free <- c.buf[:cap(c.buf)]
			}
		})
	}
	err := v.readTable(extents, free, chunks, failed)
	close(chunks)
	wg.Wait()
	if err != nil {
		return err
	}
	return firstErr
}

// readTable reads the file table's extents in order into chunks of whole
// records. A record can straddle two extents when clusters are smaller
// than records, so a partial record is carried into the next chunk.
func (v *ntfsVolume) readTable(extents []extent, free chan []byte, chunks chan<- scanChunk, failed <-chan struct{}) error {
	rs := int64(v.recordSize)
	var logical int64 // file table bytes passed so far
	var carry []byte
	for _, e := range extents {
		size := e.clusters * v.clusterSize
		for done := int64(0); done < size && logical < v.mftValid; {
			var buf []byte
			select {
			case buf = <-free:
			case <-failed:
				return nil
			}
			n0 := copy(buf, carry)
			want := min(scanChunkSize, size-done, v.mftValid-logical)
			// The read is whole sectors, which an extent always is; only
			// want bytes of it are used.
			part := buf[n0 : n0+int(min(roundUp(want, v.sectorSize), size-done))]
			if e.lcn < 0 {
				clear(part)
			} else if err := v.readAt(e.lcn*v.clusterSize+done, part); err != nil {
				return fmt.Errorf("read file table: %w", err)
			}
			done += want
			logical += want
			total := n0 + int(want)
			whole := total - total%int(rs)
			carry = append(carry[:0], buf[whole:total]...)
			chunks <- scanChunk{buf: buf[:whole], firstRec: (logical - int64(total)) / rs}
		}
	}
	return nil
}

func (v *ntfsVolume) parseChunk(idx byte, c scanChunk, emit func([]byte) error) error {
	e := encoder{b: make([]byte, 0, 256<<10)}
	e.u8(idx)
	for k := 0; k+v.recordSize <= len(c.buf); k += v.recordSize {
		num := c.firstRec + int64(k/v.recordSize)
		if num < firstUserRecord {
			continue
		}
		b := c.buf[k : k+v.recordSize]
		if string(b[:4]) != "FILE" || !applyFixups(b) {
			continue
		}
		r, ok := parseRecord(b, false)
		if !ok || !r.inUse || len(r.names) == 0 {
			continue
		}
		frn, flags := uint64(num)|uint64(r.seq)<<48, r.entryFlags()
		if r.base != 0 {
			frn = r.base
			if recordNumber(frn) < firstUserRecord {
				continue
			}
		}
		for _, n := range r.names {
			if n.namespace == namespaceDOS {
				continue
			}
			f := flags
			if r.base != 0 || !r.hasStdInfo {
				f = nameFlags(n)
			}
			e.u64(frn)
			e.u32(recordNumber(n.parent))
			e.u8(f)
			e.str(n.name)
		}
		if len(e.b) >= 240<<10 {
			if err := emit(e.b); err != nil {
				return err
			}
			e.b = append(e.b[:0], idx)
		}
	}
	if len(e.b) > 1 {
		return emit(e.b)
	}
	return nil
}

// journal is a volume's change journal.
type journal struct {
	id          uint64
	first, next int64
}

func (v *ntfsVolume) queryJournal() (journal, error) {
	out := make([]byte, 80)
	_, err := v.ioctl(fsctlQueryUSNJournal, nil, out)
	if errors.Is(err, errJournalNotActive) {
		// Most NTFS volumes keep a journal; one that does not gets a
		// small one, as Everything does.
		in := binary.LittleEndian.AppendUint64(nil, 32<<20)
		in = binary.LittleEndian.AppendUint64(in, 8<<20)
		if _, err := v.ioctl(fsctlCreateUSNJournal, in, nil); err != nil {
			return journal{}, fmt.Errorf("create change journal: %w", err)
		}
		_, err = v.ioctl(fsctlQueryUSNJournal, nil, out)
	}
	if err != nil {
		return journal{}, fmt.Errorf("query change journal: %w", err)
	}
	le := binary.LittleEndian
	return journal{id: le.Uint64(out), first: int64(le.Uint64(out[8:])), next: int64(le.Uint64(out[16:]))}, nil
}

// Change reasons the index follows.
const (
	reasonFileCreate      = 0x00000100
	reasonFileDelete      = 0x00000200
	reasonRenameNewName   = 0x00002000
	reasonBasicInfoChange = 0x00008000
	reasonHardLinkChange  = 0x00010000
	followedReasons       = reasonFileCreate | reasonFileDelete | reasonRenameNewName | reasonBasicInfoChange | reasonHardLinkChange
)

// errJournalLost means the journal cannot be followed from where the index
// is, and the volume must be listed again.
var errJournalLost = errors.New("change journal can no longer be followed")

// follow sends the volume's changes from start on, as they happen, until a
// stop event is set or sending fails.
func (v *ntfsVolume) follow(idx byte, id uint64, start int64, send func(byte, []byte) error) error {
	le := binary.LittleEndian
	buf := make([]byte, 64<<10)
	in := make([]byte, 48) // READ_USN_JOURNAL_DATA_V1
	pos := start
	for {
		le.PutUint64(in[0:], uint64(pos))
		le.PutUint32(in[8:], followedReasons)
		le.PutUint32(in[12:], 0) // every change, not only when a file closes
		le.PutUint64(in[16:], 0) // no time-out
		le.PutUint64(in[24:], 1) // wait until there is something
		le.PutUint64(in[32:], id)
		le.PutUint16(in[40:], 2)
		le.PutUint16(in[42:], 3)
		n, err := v.ioctl(fsctlReadUSNJournal, in, buf)
		switch {
		case errors.Is(err, errJournalEntryDeleted), errors.Is(err, errJournalNotActive),
			errors.Is(err, errJournalDeleteInProgress), errors.Is(err, windows.ERROR_INVALID_PARAMETER):
			return fmt.Errorf("%w: %w", errJournalLost, err)
		case err != nil:
			return err
		case n < 8:
			return errors.New("short change journal read")
		}
		next := int64(le.Uint64(buf))
		if err := v.sendChanges(idx, next, buf[8:n], send); err != nil {
			return err
		}
		pos = next
	}
}

// sendChanges turns journal records into changes: a file deleted, or a
// file whose names are re-read as they are now. A file changed several
// times in one batch is read once, at the place of its first change, so
// a folder still comes before the files made in it.
func (v *ntfsVolume) sendChanges(idx byte, next int64, recs []byte, send func(byte, []byte) error) error {
	le := binary.LittleEndian
	var order []uint64
	deleted := map[uint64]bool{}
	for len(recs) >= 8 {
		length := int(le.Uint32(recs))
		if length < 8 || length > len(recs) {
			break
		}
		r := recs[:length]
		recs = recs[length:]
		var frn uint64
		var reason uint32
		switch le.Uint16(r[4:]) {
		case 2:
			if length < 60 {
				continue
			}
			frn, reason = le.Uint64(r[8:]), le.Uint32(r[40:])
		case 3:
			if length < 76 {
				continue
			}
			frn, reason = le.Uint64(r[8:]), le.Uint32(r[56:])
		default:
			continue
		}
		if recordNumber(frn) < firstUserRecord {
			continue
		}
		if _, seen := deleted[frn]; !seen {
			order = append(order, frn)
		}
		deleted[frn] = reason&reasonFileDelete != 0
	}
	if len(order) == 0 {
		return nil
	}
	e := encoder{}
	e.u8(idx)
	e.u64(uint64(next))
	for _, frn := range order {
		if !deleted[frn] {
			flags, links, alive, err := v.current(frn)
			if errors.Is(err, errStopped) {
				return err
			}
			if alive && err == nil {
				e.u8(changeSet)
				e.u64(frn)
				e.u8(flags)
				e.u8(byte(min(len(links), 255)))
				for _, l := range links[:min(len(links), 255)] {
					e.u32(l.parent)
					e.str(l.name)
				}
				continue
			}
		}
		e.u8(changeDelete)
		e.u64(frn)
	}
	return send(frameChanges, e.b)
}
