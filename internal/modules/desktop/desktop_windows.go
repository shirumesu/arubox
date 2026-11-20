//go:build windows

package desktop

import "errors"

// saveLayout keeps the layout; the view calls it on its own thread. The
// module keeps a copy, so the settings page can save the settings, and
// take snapshots, while the view goes on changing its own.
func (m *Module) saveLayout(l Layout) {
	l = l.clone()
	m.mu.Lock()
	defer m.mu.Unlock()
	// The first change of a day keeps the layout as it was before it.
	m.snapshot(m.stored.Layout)
	next := m.stored
	next.Layout = l
	if err := m.store.SetModuleSettings(id, next); err != nil {
		m.log.Warn("save desktop layout", "error", err)
		return
	}
	m.stored = next
}

// restore puts back the layout of a snapshot, keeping the one it replaces
// as the snapshot taken before restoring, so a restore can be undone.
func (m *Module) restore(name string) error {
	l, err := m.readSnapshot(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if err := m.writeSnapshot(beforeRestore, m.stored.Layout); err != nil {
		m.mu.Unlock()
		return err
	}
	next := m.stored
	next.Layout = l
	if err := m.store.SetModuleSettings(id, next); err != nil {
		m.mu.Unlock()
		return errors.New("保存布局失败：" + err.Error())
	}
	m.stored = next
	v := m.view
	m.mu.Unlock()
	if v != nil {
		v.restore(l.clone())
	}
	return nil
}
