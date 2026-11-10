package hotkey

import (
	"errors"
	"testing"
)

func TestCanonical(t *testing.T) {
	cases := map[string]string{
		"Ctrl+Alt+K":             "Ctrl+Alt+k",
		"super+shift+space":      "Shift+Super+space",
		"Cmd+Option+Ctrl+=":      "Ctrl+Alt+Super+=",
		"Win+Page Up":            "Super+page up",
		"f13":                    "f13",
		"Shift+F5":               "Shift+f5",
		"Alt+Shift+Ctrl+Super+/": "Ctrl+Alt+Shift+Super+/",
	}
	for in, want := range cases {
		got, err := Canonical(in)
		if err != nil {
			t.Errorf("Canonical(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
		if again, _ := Canonical(got); again != got {
			t.Errorf("Canonical is not stable for %q: %q", got, again)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{"", "Ctrl+", "Ctrl+Ctrl+k", "Hyper+k", "Ctrl+f21", "Ctrl+capslock", "Ctrl+é"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", in)
		}
	}
	for _, in := range []string{"k", "Shift+k", "space", "Shift+1"} {
		if _, err := Parse(in); !errors.Is(err, ErrNeedsModifier) {
			t.Errorf("Parse(%q) = %v, want ErrNeedsModifier", in, err)
		}
	}
}

func TestSystemShortcutsAreCanonical(t *testing.T) {
	for accel, name := range systemShortcuts() {
		got, err := Canonical(accel)
		if err != nil {
			t.Errorf("system shortcut %q (%s): %v", accel, name, err)
		} else if got != accel {
			t.Errorf("system shortcut %q (%s) is not canonical, want %q", accel, name, got)
		}
	}
}
