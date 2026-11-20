package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A snapshot is the layout as it was on a day, kept as a file of its own
// named for the day, so a desktop that got into a mess can be put back.
// The first change of a day, or enabling the module, takes the day's
// snapshot of the layout before it; the last week's are kept. Snapshots
// live only in their files: the settings page reads them when it shows.

// keepDays is how many days' snapshots are kept.
const keepDays = 7

// beforeRestore names the snapshot of the layout a restore replaced.
const beforeRestore = "before-restore"

// snapshotInfo describes a snapshot for the settings page.
type snapshotInfo struct {
	name  string // its file's name without .json
	day   time.Time
	saved time.Time
	boxes int
	err   error // why the snapshot cannot be read, which keeps it from use
}

// snapshot takes today's snapshot of l unless there is one, and drops
// those older than the last week's. m.mu is held. Whatever happens, it is
// tried once a day, and why it failed shows on the settings page.
func (m *Module) snapshot(l Layout) {
	day := time.Now().Format(time.DateOnly)
	if m.snapDay == day {
		return
	}
	// A layout never laid out on any monitors has nothing to keep; the
	// day's snapshot waits for the first one that has.
	if len(l.Arrangements) == 0 {
		return
	}
	m.snapDay = day
	err := m.takeSnapshot(day, l)
	if err != nil {
		m.log.Warn("snapshot desktop layout", "error", err)
		m.snapErr = "今天的布局快照没有保存：" + err.Error()
		return
	}
	m.snapErr = ""
}

// snapshotError is why today's snapshot failed, or "".
func (m *Module) snapshotError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapErr
}

func (m *Module) takeSnapshot(day string, l Layout) error {
	_, err := os.Stat(m.snapshotPath(day))
	if err == nil {
		return nil // taken earlier today
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := m.writeSnapshot(day, l); err != nil {
		return err
	}
	days, err := m.snapshotDays()
	if err != nil {
		return err
	}
	for _, d := range days[min(len(days), keepDays):] {
		if err := os.Remove(m.snapshotPath(d.Format(time.DateOnly))); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) snapshotPath(name string) string {
	return filepath.Join(m.snaps, name+".json")
}

// snapshotDays returns the days there are snapshots of, newest first.
func (m *Module) snapshotDays() ([]time.Time, error) {
	entries, err := os.ReadDir(m.snaps)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var days []time.Time
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() {
			continue
		}
		if d, err := time.ParseInLocation(time.DateOnly, name, time.Local); err == nil {
			days = append(days, d)
		}
	}
	slices.SortFunc(days, func(a, b time.Time) int { return b.Compare(a) })
	return days, nil
}

// writeSnapshot writes a layout to a snapshot's file, replacing it whole.
func (m *Module) writeSnapshot(name string, l Layout) error {
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("编码布局失败：%w", err)
	}
	if err := os.MkdirAll(m.snaps, 0o755); err != nil {
		return err
	}
	path := m.snapshotPath(name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readSnapshot reads a snapshot's layout.
func (m *Module) readSnapshot(name string) (Layout, error) {
	var l Layout
	raw, err := os.ReadFile(m.snapshotPath(name))
	if err != nil {
		return l, fmt.Errorf("读取快照失败：%w", err)
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		return l, fmt.Errorf("快照已损坏：%w", err)
	}
	for sig, a := range l.Arrangements {
		if a == nil || a.Boxes == nil || a.Icons == nil {
			return l, fmt.Errorf("快照已损坏：缺少 %s 的布局", sig)
		}
	}
	return l, nil
}

// snapshots lists the snapshots: the one taken before the last restore
// first, then the days', newest first. One that cannot be read is listed
// with why, so the others stay of use.
func (m *Module) snapshots() ([]snapshotInfo, error) {
	days, err := m.snapshotDays()
	if err != nil {
		return nil, err
	}
	names := []string{beforeRestore}
	for _, d := range days {
		names = append(names, d.Format(time.DateOnly))
	}
	var out []snapshotInfo
	for _, name := range names {
		info := snapshotInfo{name: name}
		if d, err := time.ParseInLocation(time.DateOnly, name, time.Local); err == nil {
			info.day = d
		}
		st, err := os.Stat(m.snapshotPath(name))
		switch {
		case errors.Is(err, fs.ErrNotExist) && name == beforeRestore:
			continue
		case err != nil:
			info.err = fmt.Errorf("读取快照失败：%w", err)
		default:
			info.saved = st.ModTime()
			if l, err := m.readSnapshot(name); err != nil {
				info.err = err
			} else {
				info.boxes = len(l.Boxes)
			}
		}
		out = append(out, info)
	}
	return out, nil
}
