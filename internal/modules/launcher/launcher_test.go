package launcher

import (
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/platform"
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
		{"wx", []string{"微信", "Wx Helper Tools"}},
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

func TestPinyinInitials(t *testing.T) {
	for name, want := range map[string]string{"微信": "wx", "哔哩哔哩": "blbl", "QQ音乐": "qqyl", "Safari": "", "系统设置": "xtsz"} {
		if got := pinyinInitials(name); got != want {
			t.Errorf("pinyinInitials(%q) = %q, want %q", name, got, want)
		}
	}
	if matchScore("xtsz", "系统设置") == 0 || matchScore("wyy", "网易云音乐") == 0 {
		t.Error("pinyin initials did not match")
	}
}

func TestParseConversion(t *testing.T) {
	cases := map[string]conversion{
		"100 usd":          {100, "USD", ""},
		"$100":             {100, "USD", ""},
		"100美元":            {100, "USD", ""},
		"1,000 jpy to cny": {1000, "JPY", "CNY"},
		"50 欧元换人民币":        {50, "EUR", "CNY"},
		"¥20.5":            {20.5, "CNY", ""},
	}
	for q, want := range cases {
		got, ok := parseConversion(q)
		if !ok || got != want {
			t.Errorf("%q = %+v, %v; want %+v", q, got, ok, want)
		}
	}
	for _, q := range []string{"100", "3 pcs", "2024年", "usd", "1+2", "100 usd to xyz"} {
		if c, ok := parseConversion(q); ok {
			t.Errorf("%q parsed as %+v", q, c)
		}
	}
}

func TestConvert(t *testing.T) {
	perEuro := map[string]float64{"EUR": 1, "USD": 1.1, "CNY": 7.7, "JPY": 165}
	got := convert(conversion{amount: 11, from: "USD"}, perEuro, "2026-11-09", "CNY")
	if len(got) != 2 || got[0].Title != "77.00 CNY" || got[1].Title != "10.00 EUR" {
		t.Errorf("into home and euro: %+v", got)
	}
	got = convert(conversion{amount: 1, from: "EUR", to: "JPY"}, perEuro, "2026-11-09", "CNY")
	if len(got) != 1 || got[0].Key != "165.00" {
		t.Errorf("into a named target: %+v", got)
	}
}

func TestWorldTime(t *testing.T) {
	now := time.Date(2026, 11, 10, 6, 30, 0, 0, time.UTC)
	for _, q := range []string{"东京时间", "tokyo time", "time in tokyo", "东京", "东京现在几点"} {
		r, ok := worldTime(q, now)
		if !ok || r.Key != "15:30" || !strings.Contains(r.Subtitle, "UTC+9") {
			t.Errorf("%q = %+v, %v", q, r, ok)
		}
	}
	if r, ok := worldTime("la time", now); !ok || r.Key != "22:30" {
		t.Errorf("la time = %+v, %v", r, ok)
	}
	for _, q := range []string{"la", "safari", "time", "rome"} {
		if r, ok := worldTime(q, now); ok {
			t.Errorf("%q answered %+v", q, r)
		}
	}
}

func TestWantsTranslation(t *testing.T) {
	m := &Module{apps: []platform.App{{ID: "/Applications/Safari.app", Name: "Safari"}, {ID: "x", Name: "Visual Studio Code"}}}
	for _, q := range []string{"hello world", "你好", "serendipity", "こんにちは", "Good morning!"} {
		if !m.wantsTranslation(q) {
			t.Errorf("%q: not translated", q)
		}
	}
	for _, q := range []string{"saf", "studio", "1+2", "100 usd", "东京时间", "!ls", "hi", ""} {
		if m.wantsTranslation(q) {
			t.Errorf("%q: translated", q)
		}
	}
}

func TestSearchURL(t *testing.T) {
	maps := Keyword{URL: "https://www.google.com/maps/search/{query}"}
	google := Keyword{URL: "https://www.google.com/search?q={query}"}
	for _, c := range []struct {
		k     Keyword
		query string
		want  string
	}{
		{google, "hi there", "https://www.google.com/search?q=hi%20there"},
		{google, "c++ & go", "https://www.google.com/search?q=c%2B%2B%20%26%20go"},
		{google, "猫", "https://www.google.com/search?q=%E7%8C%AB"},
		{maps, "new york", "https://www.google.com/maps/search/new%20york"},
		{google, " ", "https://www.google.com"},
	} {
		if got := c.k.searchURL(c.query); got != c.want {
			t.Errorf("searchURL(%q) = %q, want %q", c.query, got, c.want)
		}
	}
}

func TestCheckKeywords(t *testing.T) {
	if err := checkKeywords(defaultStored().Settings); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	web := func(word, url string) Settings {
		s := defaultStored().Settings
		s.Keywords = append(s.Keywords, Keyword{Keyword: word, Name: "X", URL: url})
		return s
	}
	for name, s := range map[string]Settings{
		"space":     web("a b", "https://x.com/?q={query}"),
		"duplicate": web("GG", "https://x.com/?q={query}"),
		"builtin":   web("fy", "https://x.com/?q={query}"),
		"command":   web("!x", "https://x.com/?q={query}"),
		"scheme":    web("x", "ftp://x.com/{query}"),
		"no query":  web("x", "https://x.com/"),
		"default":   {Keywords: nil, DefaultSearch: "gg"},
	} {
		if checkKeywords(s) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
