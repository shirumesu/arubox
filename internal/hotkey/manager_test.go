package hotkey

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
)

// fakeOS stands in for the operating system's hotkey registry.
type fakeOS struct {
	bound map[string]func()
	taken map[string]bool // held by other programs
}

func (f *fakeOS) Register(accel string, cb func()) error {
	if f.taken[accel] {
		return errors.New("taken")
	}
	if _, dup := f.bound[accel]; dup {
		return errors.New("already registered by this program")
	}
	f.bound[accel] = cb
	return nil
}

func (f *fakeOS) Unregister(accel string) error {
	if _, ok := f.bound[accel]; !ok {
		return fmt.Errorf("%s not registered", accel)
	}
	delete(f.bound, accel)
	return nil
}

func (f *fakeOS) keys() []string { return slices.Sorted(maps.Keys(f.bound)) }

type fakeStore struct {
	overrides map[string]string
	failSave  bool
}

func (s *fakeStore) Hotkey(id string) (string, bool) {
	v, ok := s.overrides[id]
	return v, ok
}

func (s *fakeStore) SetHotkey(id, accel string) error {
	if s.failSave {
		return errors.New("disk full")
	}
	s.overrides[id] = accel
	return nil
}

func (s *fakeStore) ResetHotkey(id string) error {
	if s.failSave {
		return errors.New("disk full")
	}
	delete(s.overrides, id)
	return nil
}

func newTestManager(t *testing.T) (*Manager, *fakeOS, *fakeStore) {
	t.Helper()
	os := &fakeOS{bound: map[string]func(){}, taken: map[string]bool{}}
	store := &fakeStore{overrides: map[string]string{}}
	m := NewManager(os, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.system = func() map[string]string { return map[string]string{"Super+space": "聚焦搜索"} }
	m.Define("core", "arubox", []Def{
		{ID: "dashboard", Label: "打开仪表盘", Default: "Ctrl+Alt+d", Action: func() {}},
	})
	m.Define("translate", "翻译", []Def{
		{ID: "input", Label: "输入翻译", Default: "Ctrl+Alt+t", Action: func() {}},
		{ID: "selection", Label: "划词翻译", Action: func() {}},
	})
	return m, os, store
}

func status(m *Manager, id string) Status {
	for _, s := range m.List() {
		if s.ID == id {
			return s
		}
	}
	panic("no hotkey " + id)
}

func TestActivateRegistersOnlyActiveOwners(t *testing.T) {
	m, os, _ := newTestManager(t)
	m.Activate("core")
	if got := os.keys(); !slices.Equal(got, []string{"Ctrl+Alt+d"}) {
		t.Fatalf("bound = %v", got)
	}
	m.Activate("translate")
	if got := os.keys(); !slices.Equal(got, []string{"Ctrl+Alt+d", "Ctrl+Alt+t"}) {
		t.Fatalf("bound = %v", got)
	}
	m.Deactivate("translate")
	if got := os.keys(); !slices.Equal(got, []string{"Ctrl+Alt+d"}) {
		t.Fatalf("bound after deactivate = %v", got)
	}
}

func TestActivateReportsOccupiedHotkey(t *testing.T) {
	m, os, _ := newTestManager(t)
	os.taken["Ctrl+Alt+d"] = true
	m.Activate("core")
	if s := status(m, "core.dashboard"); s.Error == "" || !s.Active {
		t.Fatalf("status = %+v, want active with error", s)
	}
}

func TestSetRebindsAndPersists(t *testing.T) {
	m, os, store := newTestManager(t)
	m.Activate("core")
	if err := m.Set("core.dashboard", "super+shift+K"); err != nil {
		t.Fatal(err)
	}
	if got := os.keys(); !slices.Equal(got, []string{"Shift+Super+k"}) {
		t.Fatalf("bound = %v", got)
	}
	if store.overrides["core.dashboard"] != "Shift+Super+k" {
		t.Fatalf("stored = %q", store.overrides["core.dashboard"])
	}
}

func TestSetRejectsConflicts(t *testing.T) {
	m, os, _ := newTestManager(t)
	m.Activate("core")

	// Another hotkey of this program, even one whose module is disabled.
	err := m.Set("core.dashboard", "Ctrl+Alt+t")
	if err == nil || !strings.Contains(err.Error(), "输入翻译") {
		t.Fatalf("err = %v, want conflict with 输入翻译", err)
	}
	// A system shortcut.
	if err := m.Set("core.dashboard", "Super+space"); err == nil || !strings.Contains(err.Error(), "聚焦搜索") {
		t.Fatalf("err = %v, want conflict with 聚焦搜索", err)
	}
	// Another program.
	os.taken["Ctrl+Alt+x"] = true
	if err := m.Set("core.dashboard", "Ctrl+Alt+x"); !errors.Is(err, ErrOccupied) {
		t.Fatalf("err = %v, want ErrOccupied", err)
	}
	// The old binding survives every failure.
	if got := os.keys(); !slices.Equal(got, []string{"Ctrl+Alt+d"}) {
		t.Fatalf("bound = %v", got)
	}
	if s := status(m, "core.dashboard"); s.Accelerator != "Ctrl+Alt+d" || s.Error != "" {
		t.Fatalf("status = %+v", s)
	}
}

func TestSetProbesInactiveHotkeys(t *testing.T) {
	m, os, store := newTestManager(t)
	os.taken["Ctrl+Alt+x"] = true
	if err := m.Set("translate.selection", "Ctrl+Alt+x"); !errors.Is(err, ErrOccupied) {
		t.Fatalf("err = %v, want ErrOccupied", err)
	}
	if err := m.Set("translate.selection", "Ctrl+Alt+s"); err != nil {
		t.Fatal(err)
	}
	if len(os.bound) != 0 {
		t.Fatalf("probe left hotkeys bound: %v", os.keys())
	}
	if store.overrides["translate.selection"] != "Ctrl+Alt+s" {
		t.Fatalf("stored = %q", store.overrides["translate.selection"])
	}
}

func TestUnbindAndReset(t *testing.T) {
	m, os, store := newTestManager(t)
	m.Activate("core")
	if err := m.Set("core.dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if len(os.bound) != 0 || status(m, "core.dashboard").Accelerator != "" {
		t.Fatalf("still bound: %v", os.keys())
	}
	if v, ok := store.overrides["core.dashboard"]; !ok || v != "" {
		t.Fatalf("unbinding was not stored as an empty override")
	}
	if err := m.Reset("core.dashboard"); err != nil {
		t.Fatal(err)
	}
	if got := os.keys(); !slices.Equal(got, []string{"Ctrl+Alt+d"}) {
		t.Fatalf("bound after reset = %v", got)
	}
	if _, ok := store.overrides["core.dashboard"]; ok {
		t.Fatalf("reset left an override behind")
	}
}

func TestFailedSaveRestoresBinding(t *testing.T) {
	m, os, store := newTestManager(t)
	m.Activate("core")
	store.failSave = true
	if err := m.Set("core.dashboard", "Ctrl+Alt+q"); err == nil {
		t.Fatal("Set succeeded although saving failed")
	}
	if got := os.keys(); !slices.Equal(got, []string{"Ctrl+Alt+d"}) {
		t.Fatalf("bound = %v", got)
	}
}

func TestSuspendAndResume(t *testing.T) {
	m, os, _ := newTestManager(t)
	m.Activate("core")
	m.Suspend()
	if len(os.bound) != 0 {
		t.Fatalf("suspended but bound: %v", os.keys())
	}
	// Recording a new binding while suspended takes effect on resume.
	if err := m.Set("core.dashboard", "Ctrl+Alt+n"); err != nil {
		t.Fatal(err)
	}
	if len(os.bound) != 0 {
		t.Fatalf("suspended but bound: %v", os.keys())
	}
	m.Resume()
	if got := os.keys(); !slices.Equal(got, []string{"Ctrl+Alt+n"}) {
		t.Fatalf("bound after resume = %v", got)
	}
}
