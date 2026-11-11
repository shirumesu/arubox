package translate

import (
	"errors"
	"strings"
)

// autoDetect is the source language that lets the engine detect it.
const autoDetect = "auto"

// The translation engines.
const (
	engineGoogle = "google"
	engineDeepL  = "deepl"
	engineEdge   = "edge"
)

// The sources of the voice that reads text aloud.
const (
	// voiceSystem is the voices of the operating system.
	voiceSystem = "system"
	// voiceEdge is the online voices of the Edge browser.
	voiceEdge = "edge"
)

// Settings is what the user can change about the module.
type Settings struct {
	// Source is the language of the text, or "auto" to detect it.
	Source string `json:"source"`
	// Target is the language translations are made into.
	Target string `json:"target"`
	// Second is used instead when the detected language already is Target,
	// so reading your own language translates it out of it.
	Second string `json:"second"`
	// Engine is the translation engine: "google", "deepl" or "edge".
	Engine string `json:"engine"`
	// DeepLKey is the DeepL API key. Free keys end in ":fx".
	DeepLKey string `json:"deeplKey"`
	// Voice is where read aloud gets its voice: "system" or "edge".
	Voice string `json:"voice"`
	// KeepMinutes is how long the hidden popup is kept ready before its
	// memory is released. 0 keeps it for good.
	KeepMinutes int `json:"keepMinutes"`
}

func defaultSettings() Settings {
	return Settings{Source: autoDetect, Target: "zh-CN", Second: "en", Engine: engineGoogle, Voice: voiceSystem, KeepMinutes: 30}
}

func (s Settings) validate() error {
	if s.Source == "" || s.Target == "" || s.Second == "" || s.Target == autoDetect || s.Second == autoDetect {
		return errors.New("请选择语言")
	}
	switch {
	case s.Engine == engineDeepL && strings.TrimSpace(s.DeepLKey) == "":
		return errors.New("使用 DeepL 需要先填写 API 密钥")
	case s.Engine != engineGoogle && s.Engine != engineDeepL && s.Engine != engineEdge:
		return errors.New("未知的翻译服务")
	}
	if s.Voice != voiceSystem && s.Voice != voiceEdge {
		return errors.New("未知的朗读声音")
	}
	if s.KeepMinutes < 0 {
		return errors.New("保留时间不能为负数")
	}
	return nil
}

// sameLanguage reports whether text detected as detected is already written
// in target. Simplified and traditional Chinese count as different, so the
// one can be converted into the other.
func sameLanguage(detected, target string) bool {
	if strings.EqualFold(detected, target) {
		return true
	}
	d, _, _ := strings.Cut(strings.ToLower(detected), "-")
	t, _, _ := strings.Cut(strings.ToLower(target), "-")
	return d == t && d != "zh"
}
