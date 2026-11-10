package launcher

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The kinds of result.
const (
	kindApp         = "app"
	kindFile        = "file"
	kindFolder      = "folder"
	kindCalc        = "calc"
	kindCurrency    = "currency"
	kindTime        = "time"
	kindTranslation = "translation"
	kindCommand     = "command"
	kindWeb         = "web"
)

// Result is one row of the launcher.
type Result struct {
	// Kind is "app", "file", "folder", "calc", "currency", "time",
	// "translation", "command" or "web". Calculations, conversions and times
	// copy their Key, translations their Title.
	Kind string `json:"kind"`
	// Key is what the result acts on: an app ID, a path, a calculated value,
	// the text a translation is of, a command, or the keyword of a web
	// search, whose Title is the query.
	Key      string `json:"key"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	// Icon is a data URL, or empty for kinds the page draws itself.
	Icon string `json:"icon,omitempty"`
	// Source and Target are a translation's language codes.
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
}

// maxResults is how many rows of each source the launcher shows.
const maxResults = 8

// score rates how well text matches query, which is lowercased; 0 is no
// match.
// A whole match beats a prefix, which beats a word's prefix, the initials
// of the words, a substring and finally the query's letters in order.
// Shorter texts rank first among equals.
func score(text, query string) int {
	if query == "" || text == "" {
		return 0
	}
	lower := strings.ToLower(text)
	shorter := max(0, 100-utf8.RuneCountInString(text))
	switch {
	case lower == query:
		return 1000
	case strings.HasPrefix(lower, query):
		return 800 + shorter
	case wordPrefix(text, query):
		return 600 + shorter
	case utf8.RuneCountInString(query) >= 2 && strings.HasPrefix(initials(text), query):
		return 500 + shorter
	case strings.Contains(lower, query):
		return 400 + shorter
	case inOrder(lower, query):
		return 100 + shorter
	}
	return 0
}

// words splits text at spaces and punctuation, and before capitals that
// follow lowercase letters, as in "WeChat".
func words(text string) []string {
	var out []string
	start := -1
	var prev rune
	for i, r := range text {
		boundary := !unicode.IsLetter(r) && !unicode.IsDigit(r)
		camel := unicode.IsUpper(r) && unicode.IsLower(prev)
		switch {
		case boundary:
			if start >= 0 {
				out = append(out, text[start:i])
			}
			start = -1
		case camel && start >= 0:
			out = append(out, text[start:i])
			start = i
		case start < 0:
			start = i
		}
		prev = r
	}
	if start >= 0 {
		out = append(out, text[start:])
	}
	return out
}

func wordPrefix(text, query string) bool {
	for _, w := range words(text) {
		if strings.HasPrefix(strings.ToLower(w), query) {
			return true
		}
	}
	return false
}

func initials(text string) string {
	var b strings.Builder
	for _, w := range words(text) {
		r, _ := utf8.DecodeRuneInString(w)
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// inOrder reports whether all of query's letters appear in text in order.
func inOrder(text, query string) bool {
	for _, r := range query {
		i := strings.IndexRune(text, r)
		if i < 0 {
			return false
		}
		text = text[i+utf8.RuneLen(r):]
	}
	return true
}

// matchScore rates the best of several names, such as an app's name and
// alias, against a query. Chinese names also match by their pinyin
// initials, a little below a match of the name itself.
func matchScore(query string, names ...string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	best := 0
	for _, n := range names {
		best = max(best, score(n, q))
		if p := pinyinInitials(n); p != "" {
			best = max(best, score(p, q)-pinyinPenalty)
		}
	}
	return best
}

// pinyinPenalty ranks a match of a name's pinyin initials below the same
// kind of match of the name itself.
const pinyinPenalty = 50

// launchBoost lifts what the user opens often, without letting it beat a
// clearly better match.
func launchBoost(count int) int {
	return min(count*20, 200)
}

// recencyBoost lifts files the user opened lately.
func recencyBoost(used, now time.Time) int {
	if used.IsZero() {
		return 0
	}
	switch age := now.Sub(used); {
	case age < 24*time.Hour:
		return 150
	case age < 7*24*time.Hour:
		return 100
	case age < 30*24*time.Hour:
		return 50
	}
	return 0
}

type scored struct {
	result Result
	score  int
}

// best returns the top results, best first.
func best(all []scored, n int) []Result {
	slices.SortStableFunc(all, func(a, b scored) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.result.Title, b.result.Title))
	})
	out := make([]Result, 0, min(n, len(all)))
	for _, s := range all[:min(n, len(all))] {
		out = append(out, s.result)
	}
	return out
}
