package fileindex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// State is how far the index is.
type State string

const (
	// StateMissing means the service is not installed.
	StateMissing State = "missing"
	// StateStopped means the service is installed but not running.
	StateStopped State = "stopped"
	// StateOutdated means the service is of another version and must be
	// installed again.
	StateOutdated State = "outdated"
	// StateConnecting means the program is reaching the service.
	StateConnecting State = "connecting"
	// StateIndexing means a volume is being listed.
	StateIndexing State = "indexing"
	// StateReady means every volume is indexed and followed.
	StateReady State = "ready"
)

// Status describes the index for the settings page.
type Status struct {
	State State `json:"state"`
	// Files is how many files and folders are indexed.
	Files int `json:"files"`
	// Volumes are the indexed volumes' mount paths.
	Volumes []string `json:"volumes"`
	// Message explains a problem, such as a volume that cannot be read.
	Message string `json:"message,omitempty"`
}

// Client keeps the program's index: it loads the index saved last time,
// connects to the service and applies what the service streams.
type Client struct {
	log      *slog.Logger
	snapshot string

	mu       sync.Mutex
	excluded []string
	ex       *excludes
	vols     map[string]*volume // by GUID, the searchable ones
	listing  map[string]bool    // GUIDs being listed
	state    State
	message  string
	dirty    bool
	session  windows.Handle // set to end the current connection

	// Stop sets stop, which gives up the pipe I/O, and closes quit.
	stop windows.Handle
	quit chan struct{}
	wake chan struct{}
	done chan struct{}
}

// NewClient creates a client that saves the index at snapshot and keeps
// the folders in excluded out of it. It does nothing until Start.
func NewClient(log *slog.Logger, snapshot string, excluded []string) *Client {
	return &Client{
		log:      log,
		snapshot: snapshot,
		excluded: slices.Clone(excluded),
		ex:       newExcludes(excluded),
		vols:     map[string]*volume{},
		listing:  map[string]bool{},
		state:    StateConnecting,
		wake:     make(chan struct{}, 1),
	}
}

// Start loads the saved index and connects to the service in the
// background.
func (c *Client) Start() error {
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	c.stop, c.quit, c.done = stop, make(chan struct{}), make(chan struct{})
	go c.run()
	return nil
}

// Stop disconnects and saves the index.
func (c *Client) Stop() {
	_ = windows.SetEvent(c.stop)
	close(c.quit)
	<-c.done
	closeHandle(c.stop)
	c.save()
}

// Status reports how far the index is.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Status{State: c.state, Message: c.message}
	if st.State == StateReady && len(c.listing) > 0 {
		st.State = StateIndexing
	}
	for _, v := range c.vols {
		v.mu.RLock()
		st.Files += v.count()
		st.Volumes = append(st.Volumes, v.mount)
		v.mu.RUnlock()
	}
	slices.Sort(st.Volumes)
	return st
}

// Search returns the paths of the best limit matches for a query in
// Everything's syntax. ctx cancels it.
func (c *Client) Search(ctx context.Context, text string, limit int) []string {
	q := parseQuery(text)
	c.mu.Lock()
	vols := make([]*volume, 0, len(c.vols))
	for _, v := range c.vols {
		vols = append(vols, v)
	}
	c.mu.Unlock()
	return search(ctx, vols, q, limit)
}

// Install asks the user for administrator rights and installs the
// service, or installs it again when it is outdated or stopped.
func (c *Client) Install() error {
	sid, err := currentUser()
	if err != nil {
		return err
	}
	if err := runElevated(flagInstall + " " + sid); err != nil {
		return fmt.Errorf("安装索引服务失败：%w", err)
	}
	c.reconnect()
	return nil
}

// Remove asks the user for administrator rights and removes the service.
// The index in memory and on disk goes with it.
func (c *Client) Remove() error {
	if err := runElevated(flagRemove); err != nil {
		return fmt.Errorf("移除索引服务失败：%w", err)
	}
	c.mu.Lock()
	c.vols, c.listing, c.dirty = map[string]*volume{}, map[string]bool{}, false
	c.mu.Unlock()
	os.Remove(c.snapshot)
	c.reconnect()
	return nil
}

// SetExcluded changes the excluded folders, which lists every volume
// again.
func (c *Client) SetExcluded(excluded []string) {
	c.mu.Lock()
	if slices.Equal(c.excluded, excluded) {
		c.mu.Unlock()
		return
	}
	c.excluded, c.ex = slices.Clone(excluded), newExcludes(excluded)
	c.vols, c.listing, c.dirty = map[string]*volume{}, map[string]bool{}, false
	c.mu.Unlock()
	os.Remove(c.snapshot)
	c.reconnect()
}

// reconnect ends the current connection, if any, and connects again at
// once.
func (c *Client) reconnect() {
	c.mu.Lock()
	if c.session != 0 {
		_ = windows.SetEvent(c.session)
	}
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Client) setState(s State, message string) {
	c.mu.Lock()
	c.state, c.message = s, message
	c.mu.Unlock()
}

// saveEvery is how often a changed index is saved while running, so a
// crash loses little.
const saveEvery = 10 * time.Minute

func (c *Client) run() {
	defer close(c.done)
	c.load()
	go func() {
		t := time.NewTicker(saveEvery)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				c.save()
			case <-c.quit:
				return
			}
		}
	}()
	for {
		wait := c.connect()
		select {
		case <-c.quit:
			return
		case <-c.wake:
		case <-time.After(wait):
		}
	}
}

// connect runs one connection to the service and returns how long to wait
// before the next try.
func (c *Client) connect() time.Duration {
	name, _ := windows.UTF16PtrFromString(pipeName)
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		switch st, qerr := queryService(); {
		case qerr != nil:
			c.setState(StateStopped, qerr.Error())
		case st == serviceMissing:
			c.setState(StateMissing, "")
			return time.Hour
		case st == serviceStopped:
			c.setState(StateStopped, "")
		default:
			// Running, but not listening yet, or every instance busy.
			c.setState(StateConnecting, "")
		}
		return 3 * time.Second
	}
	defer closeHandle(h)
	session, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		c.setState(StateStopped, err.Error())
		return 3 * time.Second
	}
	c.mu.Lock()
	c.session = session
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.session = 0
		clear(c.listing)
		c.mu.Unlock()
		closeHandle(session)
	}()
	conn := &handle{h: h, stops: []windows.Handle{c.stop, session}}
	err = c.receive(conn)
	switch {
	case errors.Is(err, errOutdated):
		c.setState(StateOutdated, "")
		return time.Hour
	case errors.Is(err, errStopped):
		return 0
	}
	c.log.Warn("file index connection ended", "error", err)
	c.setState(StateConnecting, "")
	return 2 * time.Second
}

var errOutdated = errors.New("the index service is of another version")

// receive says hello and applies frames until the connection ends.
func (c *Client) receive(conn *handle) error {
	if err := writeFrame(conn, frameHello, encodeHello(c.states())); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, 1<<20)
	kind, payload, err := readFrame(r, nil)
	if err != nil {
		return err
	}
	if kind != frameHello {
		return errors.New("unexpected answer from the index service")
	}
	version, guids, err := decodeWelcome(payload)
	if err != nil {
		return err
	}
	if version != protocolVersion {
		return errOutdated
	}
	c.mu.Lock()
	for g := range c.vols {
		if !slices.Contains(guids, g) {
			delete(c.vols, g)
			c.dirty = true
		}
	}
	c.state, c.message = StateReady, ""
	ex := c.ex
	c.mu.Unlock()

	// This connection's volumes by their index in it, and the volumes
	// being listed.
	current := map[byte]*volume{}
	listing := map[byte]*volume{}
	var buf []byte
	for {
		kind, payload, err := readFrame(r, buf)
		if err != nil {
			return err
		}
		buf = payload[:0]
		if err := c.apply(kind, payload, ex, current, listing); err != nil {
			return err
		}
	}
}

func (c *Client) apply(kind byte, payload []byte, ex *excludes, current, listing map[byte]*volume) error {
	d := decoder{b: payload}
	idx := d.u8()
	switch kind {
	case frameVolume:
		guid, mount, id, full := d.str(), d.str(), d.u64(), d.u8() != 0
		if d.err != nil {
			return d.err
		}
		if full {
			listing[idx] = newVolume(guid, mount, id)
			c.mu.Lock()
			c.listing[guid] = true
			c.mu.Unlock()
			return nil
		}
		c.mu.Lock()
		v := c.vols[guid]
		c.mu.Unlock()
		if v == nil {
			return errors.New("the index service resumed a volume the program does not have")
		}
		v.mu.Lock()
		v.mount = mount
		v.mu.Unlock()
		current[idx] = v
	case frameRecords:
		v := listing[idx]
		if v == nil {
			return errors.New("records for a volume not being listed")
		}
		for len(d.b) > 0 && d.err == nil {
			frn, parent, flags, name := d.u64(), d.u32(), d.u8(), d.bytes()
			v.add(recordNumber(frn), sequence(frn), parent, flags, name)
		}
		return d.err
	case frameScanned:
		v := listing[idx]
		next := int64(d.u64())
		if v == nil || d.err != nil {
			return errors.New("bad end of a listing")
		}
		v.nextUSN = next
		v.compact(ex)
		delete(listing, idx)
		current[idx] = v
		c.mu.Lock()
		c.vols[v.guid] = v
		delete(c.listing, v.guid)
		c.dirty = true
		c.mu.Unlock()
		c.log.Info("volume indexed", "volume", v.mount, "files", v.count())
		// Listing a volume leaves behind the arrays it grew and the copy
		// compacting made, several times the index's own size; hand them
		// back to the system now rather than over the next minutes.
		debug.FreeOSMemory()
	case frameChanges:
		v := current[idx]
		if v == nil {
			return errors.New("changes for an unknown volume")
		}
		next := int64(d.u64())
		v.mu.Lock()
		for len(d.b) > 0 && d.err == nil {
			op, frn := d.u8(), d.u64()
			if op == changeDelete {
				v.remove(recordNumber(frn), sequence(frn))
				continue
			}
			flags, n := d.u8(), int(d.u8())
			links := make([]link, 0, n)
			for range n {
				links = append(links, link{parent: d.u32(), name: d.str()})
			}
			if d.err == nil {
				v.set(recordNumber(frn), sequence(frn), flags, links, ex)
			}
		}
		if d.err == nil {
			v.nextUSN = next
		}
		v.mu.Unlock()
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
		return d.err
	case frameReset:
		reason := d.str()
		delete(listing, idx)
		delete(current, idx)
		c.log.Warn("file index volume reset", "reason", reason)
		c.mu.Lock()
		c.message = reason
		c.mu.Unlock()
	}
	return d.err
}

// states says where the saved volumes left their journals.
func (c *Client) states() []volumeState {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []volumeState
	for _, v := range c.vols {
		v.mu.RLock()
		out = append(out, volumeState{GUID: v.guid, JournalID: v.journalID, NextUSN: v.nextUSN})
		v.mu.RUnlock()
	}
	return out
}

func (c *Client) load() {
	c.mu.Lock()
	excluded := c.excluded
	c.mu.Unlock()
	vols, err := loadSnapshot(c.snapshot, excluded)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.log.Info("file index snapshot not used", "error", err)
		}
		return
	}
	c.mu.Lock()
	for _, v := range vols {
		c.vols[v.guid] = v
	}
	c.mu.Unlock()
}

func (c *Client) save() {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return
	}
	excluded := c.excluded
	vols := make([]*volume, 0, len(c.vols))
	for _, v := range c.vols {
		vols = append(vols, v)
	}
	c.dirty = false
	c.mu.Unlock()
	began := time.Now()
	if err := saveSnapshot(c.snapshot, excluded, vols); err != nil {
		c.log.Warn("save file index", "error", err)
		return
	}
	c.log.Info("file index saved", "took", time.Since(began).Round(time.Millisecond))
}
