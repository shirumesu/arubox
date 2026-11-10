package module

import (
	"fmt"
	"runtime"
	"slices"
	"sync"

	"github.com/shirumesu/arubox/internal/hotkey"
)

// Store persists which modules are enabled. config.Store satisfies it.
type Store interface {
	ModuleEnabled(id string) bool
	SetModuleEnabled(id string, enabled bool) error
}

// Status describes one module for the dashboard.
type Status struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Supported is false when the module does not run on this system. Such
	// modules are listed but cannot be enabled.
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
	// Error explains why the module failed to start.
	Error string `json:"error,omitempty"`
}

type slot struct {
	module    Module
	info      Info
	supported bool
	enabled   bool
	err       string
}

// Registry is safe for concurrent use.
type Registry struct {
	ctx     Context
	store   Store
	hotkeys *hotkey.Manager

	mu    sync.Mutex
	slots []*slot
}

// NewRegistry registers modules. Hotkeys of modules that run on this system
// are declared right away so they show up in the dashboard and take part in
// conflict detection even while the module is disabled.
func NewRegistry(ctx Context, store Store, hotkeys *hotkey.Manager, modules []Module) *Registry {
	r := &Registry{ctx: ctx, store: store, hotkeys: hotkeys}
	seen := map[string]bool{}
	for _, m := range modules {
		info := m.Info()
		if seen[info.ID] {
			panic("module: duplicate id " + info.ID)
		}
		seen[info.ID] = true
		s := &slot{module: m, info: info, supported: slices.Contains(info.Platforms, runtime.GOOS)}
		if s.supported {
			hotkeys.Define(info.ID, info.Name, m.Hotkeys())
		}
		r.slots = append(r.slots, s)
	}
	return r
}

// Start enables the modules the user left enabled.
func (r *Registry) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.slots {
		if s.supported && r.store.ModuleEnabled(s.info.ID) {
			if err := r.enable(s); err != nil {
				r.ctx.Log.Error("enable module", "module", s.info.ID, "error", err)
			}
		}
	}
}

// Stop disables every running module, at shutdown.
func (r *Registry) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range slices.Backward(r.slots) {
		if s.enabled {
			r.disable(s)
		}
	}
}

// SetEnabled enables or disables a module and remembers the choice.
func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := slices.IndexFunc(r.slots, func(s *slot) bool { return s.info.ID == id })
	if i < 0 {
		return fmt.Errorf("unknown module %q", id)
	}
	s := r.slots[i]
	if enabled && !s.supported {
		return fmt.Errorf("「%s」不支持当前系统", s.info.Name)
	}
	if enabled == s.enabled {
		return nil
	}
	if enabled {
		if err := r.enable(s); err != nil {
			return fmt.Errorf("「%s」启动失败：%w", s.info.Name, err)
		}
	} else {
		r.disable(s)
	}
	if err := r.store.SetModuleEnabled(id, enabled); err != nil {
		return fmt.Errorf("保存设置失败：%w", err)
	}
	return nil
}

// List returns every module in registration order.
func (r *Registry) List() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Status, 0, len(r.slots))
	for _, s := range r.slots {
		result = append(result, Status{
			ID:          s.info.ID,
			Name:        s.info.Name,
			Description: s.info.Description,
			Supported:   s.supported,
			Enabled:     s.enabled,
			Error:       s.err,
		})
	}
	return result
}

func (r *Registry) enable(s *slot) error {
	if err := s.module.Enable(r.ctx); err != nil {
		s.err = err.Error()
		return err
	}
	s.enabled = true
	s.err = ""
	r.hotkeys.Activate(s.info.ID)
	return nil
}

func (r *Registry) disable(s *slot) {
	r.hotkeys.Deactivate(s.info.ID)
	if err := s.module.Disable(); err != nil {
		r.ctx.Log.Error("disable module", "module", s.info.ID, "error", err)
	}
	s.enabled = false
	s.err = ""
}
