package hotkey

import (
	"fmt"
	"sync"

	"github.com/shirumesu/arubox/internal/ui"
)

// System registers hotkeys with Windows through the UI thread.
type System struct {
	mu  sync.Mutex
	ids map[string]int
}

// NewSystem returns a registrar that binds hotkeys with RegisterHotKey.
func NewSystem() *System { return &System{ids: map[string]int{}} }

// Register binds accelerator; callback runs on its own goroutine.
func (s *System) Register(accelerator string, callback func()) error {
	a, err := Parse(accelerator)
	if err != nil {
		return err
	}
	vk, ok := virtualKey(a.Key)
	if !ok {
		return fmt.Errorf("不支持的按键 %q", a.Key)
	}
	var mods uint32
	if a.Mods&Ctrl != 0 {
		mods |= ui.ModCtrl
	}
	if a.Mods&Alt != 0 {
		mods |= ui.ModAlt
	}
	if a.Mods&Shift != 0 {
		mods |= ui.ModShift
	}
	if a.Mods&Super != 0 {
		mods |= ui.ModWin
	}
	id, err := ui.RegisterHotkey(mods, vk, callback)
	if err != nil {
		return ErrOccupied
	}
	s.mu.Lock()
	s.ids[a.String()] = id
	s.mu.Unlock()
	return nil
}

// Unregister removes accelerator.
func (s *System) Unregister(accelerator string) error {
	a, err := Parse(accelerator)
	if err != nil {
		return err
	}
	s.mu.Lock()
	id, ok := s.ids[a.String()]
	delete(s.ids, a.String())
	s.mu.Unlock()
	if ok {
		ui.UnregisterHotkey(id)
	}
	return nil
}

// virtualKey maps an accelerator key name to its virtual key code.
func virtualKey(key string) (uint32, bool) {
	if len(key) == 1 {
		c := key[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint32(c - 'a' + 'A'), true
		case c >= '0' && c <= '9':
			return uint32(c), true
		}
		punct := map[byte]uint32{'-': 0xBD, '=': 0xBB, '[': 0xDB, ']': 0xDD, 0x5C: 0xDC, ';': 0xBA, 0x27: 0xDE, ',': 0xBC, '.': 0xBE, '/': 0xBF, '`': 0xC0}
		v, ok := punct[c]
		return v, ok
	}
	named := map[string]uint32{
		"space": 0x20, "return": 0x0D, "tab": 0x09, "backspace": 0x08, "delete": 0x2E, "escape": 0x1B,
		"home": 0x24, "end": 0x23, "page up": 0x21, "page down": 0x22,
		"left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28,
	}
	if v, ok := named[key]; ok {
		return v, true
	}
	var n int
	if _, err := fmt.Sscanf(key, "f%d", &n); err == nil && n >= 1 && n <= 24 {
		return uint32(0x70 + n - 1), true
	}
	return 0, false
}
