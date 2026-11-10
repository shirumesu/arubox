package translate

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/platform"
)

func TestParseGoogleJoinsSentencesAndSkipsTransliteration(t *testing.T) {
	body := []byte(`{"sentences":[{"trans":"你好世界。","orig":"Hello world. "},{"trans":"第二句。\n","orig":"Second.\n"},{"translit":"Nǐ hǎo"}],"src":"en"}`)
	got, err := parseGoogle(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "你好世界。第二句。\n" || got.Source != "en" {
		t.Errorf("got %+v", got)
	}
}

func TestParseGoogleRejectsEmptyAnswers(t *testing.T) {
	for _, body := range []string{`{"sentences":[],"src":"en"}`, `<html>`, `{"sentences":[{"translit":"x"}]}`} {
		if _, err := parseGoogle([]byte(body)); err == nil {
			t.Errorf("%s: no error", body)
		}
	}
}

func TestSameLanguage(t *testing.T) {
	cases := []struct {
		detected, target string
		want             bool
	}{
		{"en", "en", true},
		{"en", "en-GB", true},
		{"zh-CN", "zh-CN", true},
		{"zh-TW", "zh-CN", false}, // traditional into simplified is a real conversion
		{"zh", "zh-CN", false},
		{"ja", "zh-CN", false},
		{"", "en", false},
	}
	for _, c := range cases {
		if got := sameLanguage(c.detected, c.target); got != c.want {
			t.Errorf("sameLanguage(%q, %q) = %v", c.detected, c.target, got)
		}
	}
}

func TestPlaceKeepsThePopupOnScreen(t *testing.T) {
	area := application.Rect{X: 0, Y: 25, Width: 1440, Height: 875}
	cases := []struct {
		name   string
		cursor application.Point
		height int
		want   application.Rect
	}{
		{"below right of the cursor", application.Point{X: 100, Y: 100}, 200, application.Rect{X: 112, Y: 112, Width: popupWidth, Height: 200}},
		{"pushed left at the right edge", application.Point{X: 1400, Y: 100}, 200, application.Rect{X: 1440 - popupGap - popupWidth, Y: 112, Width: popupWidth, Height: 200}},
		{"pushed up at the bottom", application.Point{X: 100, Y: 880}, 300, application.Rect{X: 112, Y: 900 - popupGap - 300, Width: popupWidth, Height: 300}},
		{"cut to the screen height", application.Point{X: 100, Y: 100}, 2000, application.Rect{X: 112, Y: 25 + popupGap, Width: popupWidth, Height: 875 - 2*popupGap}},
		{"on a screen left of the primary one", application.Point{X: -1900, Y: 100}, 200, application.Rect{X: -1900 + popupGap, Y: 112, Width: popupWidth, Height: 200}},
	}
	for _, c := range cases {
		a := area
		if c.cursor.X < 0 {
			a = application.Rect{X: -1920, Y: 0, Width: 1920, Height: 1080}
		}
		if got := place(c.cursor, c.height, a); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestParseDeepL(t *testing.T) {
	got, err := parseDeepL([]byte(`{"translations":[{"detected_source_language":"ZH","text":"Hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Hello" || got.Source != "zh-CN" {
		t.Errorf("got %+v", got)
	}
	if _, err := parseDeepL([]byte(`{"translations":[]}`)); err == nil {
		t.Error("empty answer: no error")
	}
}

func TestDeepLLanguages(t *testing.T) {
	for code, want := range map[string]string{"zh-CN": "ZH-HANS", "zh-TW": "ZH-HANT", "en": "EN-US", "pt": "PT-BR", "ja": "JA"} {
		if got := deeplTarget(code); got != want {
			t.Errorf("deeplTarget(%s) = %s, want %s", code, got, want)
		}
	}
	for code, want := range map[string]string{"zh-CN": "ZH", "zh-TW": "ZH", "en": "EN"} {
		if got := deeplSource(code); got != want {
			t.Errorf("deeplSource(%s) = %s, want %s", code, got, want)
		}
	}
}

func TestParagraphs(t *testing.T) {
	line := func(text string, y float64) platform.TextLine {
		return platform.TextLine{Text: text, Box: platform.Box{X: 10, Y: y, Width: 300, Height: 20}}
	}
	cases := []struct {
		name  string
		lines []platform.TextLine
		want  string
	}{
		{"joins a broken sentence", []platform.TextLine{line("The quick brown", 0), line("fox jumps.", 24)}, "The quick brown fox jumps."},
		{"mends hyphenation", []platform.TextLine{line("transla-", 0), line("tion works", 24)}, "translation works"},
		{"keeps paragraphs apart", []platform.TextLine{line("First.", 0), line("Second.", 60)}, "First.\nSecond."},
		{"joins Chinese without spaces", []platform.TextLine{line("这是一句很长", 0), line("的话。", 24)}, "这是一句很长的话。"},
		{"drops OCR spaces between Chinese", []platform.TextLine{line("中 文 识 别 OCR 结果", 0)}, "中文识别 OCR 结果"},
		{"starts again for a line above", []platform.TextLine{line("Right column", 100), line("Left column", 0)}, "Right column\nLeft column"},
	}
	for _, c := range cases {
		if got := paragraphs(c.lines); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
