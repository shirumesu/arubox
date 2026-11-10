package launcher

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
)

// Translator translates text for the launcher. The core provides it from
// the translation module.
type Translator interface {
	// Translate translates text into the translation module's target
	// language. ok is false while that module is off.
	Translate(ctx context.Context, text string) (t Translation, ok bool, err error)
	// Open shows text in the translation popup.
	Open(text string)
}

// Translation is a translated text.
type Translation struct {
	Text   string
	Source string // language codes, such as "en" and "zh-CN"
	Target string
	Engine string // the engine's display name
}

// maxTranslated bounds what the launcher sends for translation; longer
// text belongs in the popup.
const maxTranslated = 2000

// translate returns a row with the query's translation, when the query reads
// as text to translate rather than something to find or calculate: a
// sentence, Chinese, Japanese or Korean, or a word that names no app.
func (m *Module) translate(ctx context.Context, query string) []Result {
	q := strings.TrimSpace(query)
	if m.translator == nil || !m.wantsTranslation(q) {
		return nil
	}
	t, ok, err := m.translator.Translate(ctx, q)
	switch {
	case !ok || errors.Is(err, context.Canceled) || ctx.Err() != nil:
		return nil
	case err != nil:
		return []Result{{Kind: kindTranslation, Title: "翻译失败", Subtitle: err.Error()}}
	case strings.EqualFold(strings.TrimSpace(t.Text), q):
		// Nothing to show for text that is already in the target language.
		return nil
	}
	return []Result{{
		Kind:     kindTranslation,
		Key:      q,
		Title:    t.Text,
		Subtitle: t.Engine,
		Source:   t.Source,
		Target:   t.Target,
	}}
}

func (m *Module) wantsTranslation(q string) bool {
	if q == "" || len(q) > maxTranslated || strings.HasPrefix(q, "!") {
		return false
	}
	if _, ok := calculate(q); ok {
		return false
	}
	if _, ok := parseConversion(q); ok {
		return false
	}
	if _, ok := worldTime(q, time.Now()); ok {
		return false
	}
	if strings.ContainsFunc(q, unicode.IsSpace) || strings.ContainsFunc(q, isEastAsian) {
		return true
	}
	if len([]rune(q)) < 3 {
		return false
	}
	// A word that starts an app's name is a search, not text.
	m.mu.Lock()
	apps := m.apps
	m.mu.Unlock()
	for _, a := range apps {
		if matchScore(q, a.Name, a.Alias) >= wordStartScore {
			return false
		}
	}
	return true
}

// wordStartScore is the score of a match at the start of a name's word.
const wordStartScore = 600

func isEastAsian(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}
