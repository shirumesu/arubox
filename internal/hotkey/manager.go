// Package hotkey owns every global hotkey: their definitions, the user's
// bindings, conflict detection and registration with the operating system.
package hotkey

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// Registrar binds accelerators with the operating system. Wails'
// GlobalShortcutManager satisfies it.
type Registrar interface {
	Register(accelerator string, callback func()) error
	Unregister(accelerator string) error
}

// Store persists the user's bindings. config.Store satisfies it.
type Store interface {
	Hotkey(id string) (accelerator string, ok bool)
	SetHotkey(id, accelerator string) error
	ResetHotkey(id string) error
}

// Def declares one hotkey of an owner (the core or a module).
type Def struct {
	// ID is unique within the owner.
	ID string
	// Label is shown in the dashboard.
	Label string
	// Default is the canonical accelerator used until the user changes it.
	// Empty means unbound by default.
	Default string
	// Action runs, on its own goroutine, when the hotkey is pressed.
	Action func()
}

// Status describes one hotkey for the dashboard.
type Status struct {
	ID          string `json:"id"`
	Owner       string `json:"owner"`
	OwnerName   string `json:"ownerName"`
	Label       string `json:"label"`
	Accelerator string `json:"accelerator"`
	Default     string `json:"default"`
	// Active is true when the owner is running, so the hotkey should work.
	Active bool `json:"active"`
	// Error explains why an active hotkey is not working.
	Error string `json:"error,omitempty"`
}

// ErrOccupied is returned when the operating system refuses a hotkey.
var ErrOccupied = errors.New("该快捷键已被系统或其他程序占用")

type entry struct {
	id        string
	owner     string
	ownerName string
	def       Def
	active    bool
	bound     string // accelerator registered with the OS, "" when none
	err       string
}

// Manager is safe for concurrent use.
type Manager struct {
	reg    Registrar
	store  Store
	log    *slog.Logger
	system func() map[string]string

	mu        sync.Mutex
	entries   map[string]*entry
	order     []*entry
	suspended bool
}

// NewManager creates a manager that registers hotkeys through reg and keeps
// the user's bindings in store.
func NewManager(reg Registrar, store Store, log *slog.Logger) *Manager {
	return &Manager{
		reg:     reg,
		store:   store,
		log:     log,
		system:  systemShortcuts,
		entries: map[string]*entry{},
	}
}

// Define declares the hotkeys of an owner. Hotkeys are listed and take part
// in conflict detection from now on, but only work after Activate.
func (m *Manager) Define(owner, ownerName string, defs []Def) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range defs {
		id := owner + "." + d.ID
		if _, dup := m.entries[id]; dup {
			panic("hotkey: duplicate id " + id)
		}
		e := &entry{id: id, owner: owner, ownerName: ownerName, def: d}
		m.entries[id] = e
		m.order = append(m.order, e)
	}
}

// Activate registers the owner's hotkeys with the operating system. A hotkey
// that cannot be registered is reported through its Status.
func (m *Manager) Activate(owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.order {
		if e.owner == owner && !e.active {
			e.active = true
			m.bind(e)
		}
	}
}

// Deactivate unregisters the owner's hotkeys.
func (m *Manager) Deactivate(owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.order {
		if e.owner == owner {
			m.unbind(e)
			e.active = false
			e.err = ""
		}
	}
}

// Set binds a hotkey to accelerator, or unbinds it when accelerator is empty.
// It fails, leaving the old binding in place, when the accelerator is invalid
// or conflicts with another hotkey, a system shortcut or another program.
func (m *Manager) Set(id, accelerator string) error {
	return m.change(id, accelerator, func(canonical string) error { return m.store.SetHotkey(id, canonical) })
}

// Reset restores a hotkey's default binding.
func (m *Manager) Reset(id string) error {
	m.mu.Lock()
	e, ok := m.entries[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown hotkey %q", id)
	}
	return m.change(id, e.def.Default, func(string) error { return m.store.ResetHotkey(id) })
}

// change validates and applies accelerator, then calls persist with its
// canonical form, undoing the change if persisting fails.
func (m *Manager) change(id, accelerator string, persist func(canonical string) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return fmt.Errorf("unknown hotkey %q", id)
	}
	if accelerator != "" {
		canonical, err := Canonical(accelerator)
		if err != nil {
			return err
		}
		accelerator = canonical
		if err := m.checkConflicts(e, accelerator); err != nil {
			return err
		}
	}
	old := m.effective(e)
	if err := m.apply(e, accelerator); err != nil {
		return err
	}
	if err := persist(accelerator); err != nil {
		if rollbackErr := m.apply(e, old); rollbackErr != nil {
			m.log.Error("restore hotkey after failed save", "id", id, "error", rollbackErr)
		}
		return fmt.Errorf("保存设置失败：%w", err)
	}
	return nil
}

// checkConflicts reports whether accelerator is taken by another hotkey of
// this program (enabled or not, so enabling a module later cannot clash) or
// by a known system shortcut.
func (m *Manager) checkConflicts(e *entry, accelerator string) error {
	for _, other := range m.order {
		if other != e && m.effective(other) == accelerator {
			return fmt.Errorf("与「%s · %s」冲突", other.ownerName, other.def.Label)
		}
	}
	if name, ok := m.system()[accelerator]; ok {
		return fmt.Errorf("与系统快捷键「%s」冲突", name)
	}
	return nil
}

// apply makes accelerator the live binding of e. For a hotkey that is not
// currently registered it only probes whether the OS would accept it.
func (m *Manager) apply(e *entry, accelerator string) error {
	if !e.active || m.suspended {
		if accelerator == "" || accelerator == e.bound {
			return nil
		}
		if err := m.reg.Register(accelerator, func() {}); err != nil {
			return ErrOccupied
		}
		if err := m.reg.Unregister(accelerator); err != nil {
			m.log.Error("release probed hotkey", "accelerator", accelerator, "error", err)
		}
		return nil
	}
	previous := e.bound
	m.unbind(e)
	if accelerator == "" {
		e.err = ""
		return nil
	}
	if err := m.reg.Register(accelerator, e.def.Action); err != nil {
		if previous != "" {
			if restoreErr := m.reg.Register(previous, e.def.Action); restoreErr == nil {
				e.bound = previous
			} else {
				m.log.Error("restore previous hotkey", "id", e.id, "accelerator", previous, "error", restoreErr)
			}
		}
		return ErrOccupied
	}
	e.bound = accelerator
	e.err = ""
	return nil
}

// Suspend unregisters every hotkey, so the dashboard can record a key
// combination that is currently bound without triggering it.
func (m *Manager) Suspend() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.suspended {
		return
	}
	for _, e := range m.order {
		m.unbind(e)
	}
	m.suspended = true
}

// Resume re-registers the hotkeys released by Suspend.
func (m *Manager) Resume() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.suspended {
		return
	}
	m.suspended = false
	for _, e := range m.order {
		if e.active {
			m.bind(e)
		}
	}
}

// UnregisterAll releases every hotkey at shutdown.
func (m *Manager) UnregisterAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.order {
		m.unbind(e)
	}
}

// List returns every defined hotkey in definition order.
func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Status, 0, len(m.order))
	for _, e := range m.order {
		result = append(result, Status{
			ID:          e.id,
			Owner:       e.owner,
			OwnerName:   e.ownerName,
			Label:       e.def.Label,
			Accelerator: m.effective(e),
			Default:     e.def.Default,
			Active:      e.active,
			Error:       e.err,
		})
	}
	return result
}

// effective is the accelerator the user wants for e.
func (m *Manager) effective(e *entry) string {
	if accel, ok := m.store.Hotkey(e.id); ok {
		return accel
	}
	return e.def.Default
}

// bind registers e's binding with the OS, recording why when it cannot.
func (m *Manager) bind(e *entry) {
	if m.suspended {
		return
	}
	e.err = ""
	accel := m.effective(e)
	if accel == "" {
		return
	}
	if _, err := Parse(accel); err != nil {
		e.err = "设置文件中的快捷键无效：" + err.Error()
		return
	}
	for _, other := range m.order {
		if other != e && other.bound == accel {
			e.err = fmt.Sprintf("与「%s · %s」冲突", other.ownerName, other.def.Label)
			return
		}
	}
	if err := m.reg.Register(accel, e.def.Action); err != nil {
		m.log.Warn("register hotkey", "id", e.id, "accelerator", accel, "error", err)
		e.err = ErrOccupied.Error()
		return
	}
	e.bound = accel
}

func (m *Manager) unbind(e *entry) {
	if e.bound == "" {
		return
	}
	if err := m.reg.Unregister(e.bound); err != nil {
		m.log.Error("unregister hotkey", "id", e.id, "accelerator", e.bound, "error", err)
	}
	e.bound = ""
}
