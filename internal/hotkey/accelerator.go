package hotkey

import (
	"errors"
	"fmt"
	"strings"
)

// Mod is a set of modifier keys.
type Mod uint8

// Modifier keys. Super is Command on macOS and the Windows key on Windows.
const (
	Ctrl Mod = 1 << iota
	Alt
	Shift
	Super
)

// modOrder fixes the order modifiers appear in canonical strings. The names
// are also the ones Wails' accelerator parser understands, and they mean the
// same physical keys on both platforms.
var modOrder = []struct {
	mod  Mod
	name string
}{
	{Ctrl, "Ctrl"},
	{Alt, "Alt"},
	{Shift, "Shift"},
	{Super, "Super"},
}

// keys is the set of keys a global hotkey may use: the ones every supported
// platform can register. Names follow Wails' accelerator syntax.
var keys = func() map[string]struct{} {
	set := map[string]struct{}{}
	add := func(names ...string) {
		for _, n := range names {
			set[n] = struct{}{}
		}
	}
	for c := 'a'; c <= 'z'; c++ {
		add(string(c))
	}
	for c := '0'; c <= '9'; c++ {
		add(string(c))
	}
	add("-", "=", "[", "]", "\\", ";", "'", ",", ".", "/", "`")
	add("space", "return", "tab", "backspace", "delete", "escape",
		"home", "end", "page up", "page down", "left", "right", "up", "down")
	for i := 1; i <= 20; i++ {
		add(fmt.Sprintf("f%d", i))
	}
	return set
}()

// Accelerator is a parsed hotkey: a set of modifiers plus one key.
type Accelerator struct {
	Mods Mod
	Key  string
}

// ErrNeedsModifier is returned for hotkeys that would swallow ordinary typing.
var ErrNeedsModifier = errors.New("快捷键需要包含 Ctrl、Alt/Option 或 Cmd/Win 中的至少一个（F 键除外）")

// Parse reads an accelerator such as "Ctrl+Alt+k" or "Super+Shift+space".
// Modifier and key names are case-insensitive.
func Parse(s string) (Accelerator, error) {
	parts := strings.Split(s, "+")
	key := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if _, ok := keys[key]; !ok {
		return Accelerator{}, fmt.Errorf("不支持的按键 %q", parts[len(parts)-1])
	}
	var a Accelerator
	a.Key = key
	for _, p := range parts[:len(parts)-1] {
		m, ok := parseMod(strings.TrimSpace(p))
		if !ok {
			return Accelerator{}, fmt.Errorf("不支持的修饰键 %q", p)
		}
		if a.Mods&m != 0 {
			return Accelerator{}, fmt.Errorf("修饰键 %q 重复", p)
		}
		a.Mods |= m
	}
	if a.Mods&(Ctrl|Alt|Super) == 0 && !isFunctionKey(key) {
		return Accelerator{}, ErrNeedsModifier
	}
	return a, nil
}

// String returns the canonical form, e.g. "Ctrl+Shift+k". Two accelerators
// are the same hotkey exactly when their canonical forms are equal.
func (a Accelerator) String() string {
	var b strings.Builder
	for _, m := range modOrder {
		if a.Mods&m.mod != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	b.WriteString(a.Key)
	return b.String()
}

// Canonical parses s and returns its canonical form.
func Canonical(s string) (string, error) {
	a, err := Parse(s)
	if err != nil {
		return "", err
	}
	return a.String(), nil
}

func parseMod(name string) (Mod, bool) {
	switch strings.ToLower(name) {
	case "ctrl", "control":
		return Ctrl, true
	case "alt", "option":
		return Alt, true
	case "shift":
		return Shift, true
	case "super", "cmd", "command", "win":
		return Super, true
	}
	return 0, false
}

func isFunctionKey(key string) bool {
	return len(key) >= 2 && key[0] == 'f' && key[1] >= '1' && key[1] <= '9'
}
