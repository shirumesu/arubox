package launcher

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestScoreOrder(t *testing.T) {
	// Each name should rank above the next for the query.
	cases := []struct {
		query string
		names []string
	}{
		{"code", []string{"Code", "Codex", "Visual Studio Code", "Xcode"}},
		{"vsc", []string{"Visual Studio Code", "VS Clip"}},
		{"chat", []string{"Chat", "WeChat", "Snapchatter"}},
		{"音乐", []string{"音乐", "网易云音乐"}},
	}
	for _, c := range cases {
		for i := 1; i < len(c.names); i++ {
			a, b := matchScore(c.query, c.names[i-1]), matchScore(c.query, c.names[i])
			if a <= b {
				t.Errorf("%q: %q scored %d, not above %q at %d", c.query, c.names[i-1], a, c.names[i], b)
			}
		}
	}
	if s := matchScore("zzz", "Safari"); s != 0 {
		t.Errorf("unrelated name scored %d", s)
	}
	if s := matchScore("sfr", "Safari"); s == 0 {
		t.Error("letters in order did not match")
	}
}

func TestCalculate(t *testing.T) {
	cases := map[string]string{
		"1+2*3":     "7",
		"(1+2)*3":   "9",
		"2^10":      "1024",
		"2^3^2":     "512",
		"-3+5":      "2",
		"10 % 4":    "2",
		"0.1+0.2":   "0.3",
		"7÷2":       "3.5",
		"1,000 × 3": "3000",
		"（1+1）*2":   "4",
		"1/3":       "0.333333333333",
	}
	for expr, want := range cases {
		v, ok := calculate(expr)
		if !ok {
			t.Errorf("%q: not calculated", expr)
			continue
		}
		if got := formatNumber(v); got != want {
			t.Errorf("%q = %s, want %s", expr, got, want)
		}
	}
	for _, expr := range []string{"42", "safari", "1+", "2*(3", "1/0", "", "a-b"} {
		if _, ok := calculate(expr); ok {
			t.Errorf("%q: calculated", expr)
		}
	}
}

func TestPlace(t *testing.T) {
	area := application.Rect{X: 0, Y: 25, Width: 1440, Height: 875}
	got := place(300, area)
	if got.X != 400 || got.Y != 25+192 || got.Height != 300 {
		t.Errorf("place = %+v", got)
	}
	// Too tall to fit below the usual top: moves up, never off the area.
	if got := place(800, area); got.Y != 100 || got.Height != 800 {
		t.Errorf("tall place = %+v", got)
	}
	if got := place(2000, area); got.Y != 25 || got.Height != 875 {
		t.Errorf("huge place = %+v", got)
	}
}
