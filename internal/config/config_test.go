package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	s, err := Open(path, discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetHotkey("core.dashboard", "Ctrl+Alt+d"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHotkey("translate.input", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetModuleEnabled("translate", true); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path, discard)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := reopened.Hotkey("core.dashboard"); !ok || v != "Ctrl+Alt+d" {
		t.Errorf("core.dashboard = %q, %v", v, ok)
	}
	if v, ok := reopened.Hotkey("translate.input"); !ok || v != "" {
		t.Errorf("translate.input = %q, %v; want explicit empty override", v, ok)
	}
	if !reopened.ModuleEnabled("translate") {
		t.Error("translate not enabled after reopen")
	}

	if err := reopened.ResetHotkey("core.dashboard"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Hotkey("core.dashboard"); ok {
		t.Error("override survived reset")
	}
}

func TestCorruptFileIsMovedAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path, discard)
	if err != nil {
		t.Fatal(err)
	}
	if s.ModuleEnabled("anything") {
		t.Error("corrupt settings were not reset")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "config.json.broken-*"))
	if len(matches) != 1 {
		t.Fatalf("backups = %v, want one", matches)
	}
}

func TestFailedWriteKeepsMemoryUnchanged(t *testing.T) {
	dir := t.TempDir()
	// A directory in place of the file makes the final rename fail.
	path := filepath.Join(dir, "config.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Store{path: path}
	if err := s.SetModuleEnabled("translate", true); err == nil {
		t.Fatal("write into a directory succeeded")
	}
	if s.ModuleEnabled("translate") {
		t.Error("failed write changed the in-memory settings")
	}
}
