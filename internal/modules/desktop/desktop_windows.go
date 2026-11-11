//go:build windows

package desktop

// saveLayout keeps the layout; the view calls it on its own thread.
func (m *Module) saveLayout(l Layout) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.stored
	next.Layout = l
	if err := m.store.SetModuleSettings(id, next); err != nil {
		m.log.Warn("save desktop layout", "error", err)
		return
	}
	m.stored = next
}
