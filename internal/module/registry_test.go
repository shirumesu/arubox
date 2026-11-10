package module

import (
	"errors"
	"io"
	"log/slog"
	"runtime"
	"testing"

	"github.com/shirumesu/arubox/internal/hotkey"
)

type fakeModule struct {
	info      Info
	enableErr error
	running   bool
}

func (m *fakeModule) Info() Info { return m.info }

func (m *fakeModule) Hotkeys() []hotkey.Def {
	return []hotkey.Def{{ID: "go", Label: "Go", Default: "Ctrl+Alt+" + m.info.ID[:1], Action: func() {}}}
}

func (m *fakeModule) Enable(Context) error {
	if m.enableErr != nil {
		return m.enableErr
	}
	m.running = true
	return nil
}

func (m *fakeModule) Disable() error {
	m.running = false
	return nil
}

type memStore struct{ enabled map[string]bool }

func (s *memStore) ModuleEnabled(id string) bool { return s.enabled[id] }

func (s *memStore) SetModuleEnabled(id string, on bool) error {
	s.enabled[id] = on
	return nil
}

type nopRegistrar struct{}

func (nopRegistrar) Register(string, func()) error { return nil }
func (nopRegistrar) Unregister(string) error       { return nil }

type nopHotkeyStore struct{}

func (nopHotkeyStore) Hotkey(string) (string, bool)   { return "", false }
func (nopHotkeyStore) SetHotkey(string, string) error { return nil }
func (nopHotkeyStore) ResetHotkey(string) error       { return nil }

func setup(modules ...Module) (*Registry, *memStore, *hotkey.Manager) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hk := hotkey.NewManager(nopRegistrar{}, nopHotkeyStore{}, log)
	store := &memStore{enabled: map[string]bool{}}
	return NewRegistry(Context{Log: log}, store, hk, modules), store, hk
}

func TestStartEnablesRememberedModules(t *testing.T) {
	a := &fakeModule{info: Info{ID: "alpha", Platforms: []string{runtime.GOOS}}}
	b := &fakeModule{info: Info{ID: "beta", Platforms: []string{runtime.GOOS}}}
	r, store, hk := setup(a, b)
	store.enabled["beta"] = true
	r.Start()
	if a.running || !b.running {
		t.Fatalf("alpha running=%v beta running=%v", a.running, b.running)
	}
	for _, h := range hk.List() {
		if want := h.Owner == "beta"; h.Active != want {
			t.Errorf("hotkey %s active=%v, want %v", h.ID, h.Active, want)
		}
	}
	r.Stop()
	if b.running {
		t.Error("Stop left beta running")
	}
}

func TestSetEnabled(t *testing.T) {
	a := &fakeModule{info: Info{ID: "alpha", Platforms: []string{runtime.GOOS}}}
	r, store, _ := setup(a)
	if err := r.SetEnabled("alpha", true); err != nil {
		t.Fatal(err)
	}
	if !a.running || !store.enabled["alpha"] || !r.List()[0].Enabled {
		t.Fatal("alpha not enabled")
	}
	if err := r.SetEnabled("alpha", false); err != nil {
		t.Fatal(err)
	}
	if a.running || store.enabled["alpha"] {
		t.Fatal("alpha not disabled")
	}
}

func TestFailedEnableIsReported(t *testing.T) {
	a := &fakeModule{info: Info{ID: "alpha", Platforms: []string{runtime.GOOS}}, enableErr: errors.New("boom")}
	r, store, _ := setup(a)
	if err := r.SetEnabled("alpha", true); err == nil {
		t.Fatal("enable succeeded")
	}
	s := r.List()[0]
	if s.Enabled || s.Error != "boom" || store.enabled["alpha"] {
		t.Fatalf("status = %+v, stored = %v", s, store.enabled["alpha"])
	}
}

func TestUnsupportedModule(t *testing.T) {
	a := &fakeModule{info: Info{ID: "alpha", Platforms: []string{"plan9"}}}
	r, _, hk := setup(a)
	if s := r.List()[0]; s.Supported {
		t.Fatal("plan9 module reported as supported")
	}
	if err := r.SetEnabled("alpha", true); err == nil {
		t.Fatal("unsupported module was enabled")
	}
	if len(hk.List()) != 0 {
		t.Fatal("unsupported module declared hotkeys")
	}
}
