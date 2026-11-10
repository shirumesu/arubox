// Package config persists user settings as a single JSON file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Config is the on-disk shape of the settings file.
type Config struct {
	// Hotkeys holds user overrides keyed by hotkey id. An empty string means
	// the user explicitly unbound the hotkey; a missing key means "use the
	// default".
	Hotkeys map[string]string `json:"hotkeys,omitempty"`
	// Modules holds per-module state keyed by module id.
	Modules map[string]Module `json:"modules,omitempty"`
}

// Module is the persisted state of one module.
type Module struct {
	Enabled bool `json:"enabled"`
	// Settings is the module's own settings, in a shape only it knows.
	Settings json.RawMessage `json:"settings,omitempty"`
}

// Store is a concurrency-safe view of the settings file. Every mutation is
// written to disk before it becomes visible.
type Store struct {
	path string
	mu   sync.RWMutex
	data Config
}

// Open loads the settings file at path. A missing file yields empty settings.
// An unreadable (corrupt) file is moved aside so the user keeps a copy, and
// empty settings are used instead.
func Open(path string, log *slog.Logger) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		backup := fmt.Sprintf("%s.broken-%s", path, time.Now().Format("20060102-150405"))
		if renameErr := os.Rename(path, backup); renameErr != nil {
			return nil, fmt.Errorf("settings file is corrupt (%w) and could not be moved aside: %w", err, renameErr)
		}
		log.Warn("settings file is corrupt, starting with defaults", "error", err, "backup", backup)
		s.data = Config{}
	}
	return s, nil
}

// Hotkey returns the user's override for a hotkey. ok is false when the user
// has not overridden it.
func (s *Store) Hotkey(id string) (accelerator string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	accelerator, ok = s.data.Hotkeys[id]
	return accelerator, ok
}

// SetHotkey stores an override for a hotkey. An empty accelerator records
// that the hotkey is deliberately unbound.
func (s *Store) SetHotkey(id, accelerator string) error {
	return s.update(func(c *Config) {
		if c.Hotkeys == nil {
			c.Hotkeys = map[string]string{}
		}
		c.Hotkeys[id] = accelerator
	})
}

// ResetHotkey removes the override for a hotkey so its default applies.
func (s *Store) ResetHotkey(id string) error {
	return s.update(func(c *Config) {
		delete(c.Hotkeys, id)
	})
}

// ModuleEnabled reports whether the user enabled a module.
func (s *Store) ModuleEnabled(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Modules[id].Enabled
}

// SetModuleEnabled records whether a module is enabled.
func (s *Store) SetModuleEnabled(id string, enabled bool) error {
	return s.update(func(c *Config) {
		if c.Modules == nil {
			c.Modules = map[string]Module{}
		}
		m := c.Modules[id]
		m.Enabled = enabled
		c.Modules[id] = m
	})
}

// ModuleSettings decodes a module's settings into v. Fields the file does not
// have keep the values v already holds, so callers pass in the defaults.
func (s *Store) ModuleSettings(id string, v any) error {
	s.mu.RLock()
	raw := s.data.Modules[id].Settings
	s.mu.RUnlock()
	if raw == nil {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("read %s settings: %w", id, err)
	}
	return nil
}

// SetModuleSettings stores a module's settings.
func (s *Store) SetModuleSettings(id string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s settings: %w", id, err)
	}
	return s.update(func(c *Config) {
		if c.Modules == nil {
			c.Modules = map[string]Module{}
		}
		m := c.Modules[id]
		m.Settings = raw
		c.Modules[id] = m
	})
}

// update applies fn to a copy of the settings, writes the copy to disk and
// only then makes it current, so a failed write leaves memory and disk equal.
func (s *Store) update(fn func(c *Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := Config{
		Hotkeys: maps.Clone(s.data.Hotkeys),
		Modules: maps.Clone(s.data.Modules),
	}
	fn(&next)
	if err := writeFile(s.path, next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// writeFile replaces the settings file atomically.
func writeFile(path string, c Config) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	defer os.Remove(tmp.Name()) // fails harmlessly once renamed
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}
