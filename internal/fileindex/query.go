package fileindex

import (
	"bytes"
	"strings"
)

// A query follows Everything's syntax:
//
//	report pdf      both words, anywhere in the name
//	pdf | docx      either word
//	!draft          not this word
//	"two words"     a phrase with a space
//	*.pdf, a?c      wildcards, matched against the whole name
//	docs\report     a backslash matches against the whole path
//	ext:pdf;docx    one of these extensions
//	file:, folder:  only files or only folders, optionally with a word
//
// Case is ignored.
type query struct {
	// groups must all match; a group matches when any of its terms does.
	groups [][]term
	// anchor is a word every result's name contains, which a search looks
	// for directly in the names, or "" when there is none.
	anchor string
	// rank is the word results are ranked by.
	rank string
}

type termKind byte

const (
	termName termKind = iota
	termPath
	termWildName
	termWildPath
	termExt
	termFile
	termFolder
)

type term struct {
	kind termKind
	not  bool
	text string   // folded
	exts []string // folded, for termExt
}

func parseQuery(s string) query {
	var q query
	tokens := tokenize(s)
	or := false
	for _, tok := range tokens {
		if tok == "|" {
			or = len(q.groups) > 0
			continue
		}
		var ts []term
		for alt := range strings.SplitSeq(tok, "|") {
			ts = append(ts, parseTerm(alt)...)
		}
		if len(ts) == 0 {
			continue
		}
		if or {
			last := &q.groups[len(q.groups)-1]
			*last = append(*last, ts...)
			or = false
			continue
		}
		// A term such as "file:report" yields two terms, both required.
		if !strings.Contains(tok, "|") {
			for _, t := range ts {
				q.groups = append(q.groups, []term{t})
			}
			continue
		}
		q.groups = append(q.groups, ts)
	}
	for _, g := range q.groups {
		if len(g) != 1 || g[0].not {
			continue
		}
		t := g[0]
		if t.kind == termName && len(t.text) > len(q.anchor) {
			q.anchor = t.text
		}
		if q.rank == "" && (t.kind == termName || t.kind == termPath) {
			q.rank = t.text[strings.LastIndexByte(t.text, '\\')+1:]
		}
	}
	return q
}

// tokenize splits at spaces outside quotes and keeps a lone | as a token.
func tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func parseTerm(s string) []term {
	not := false
	for strings.HasPrefix(s, "!") {
		not, s = !not, s[1:]
	}
	f := string(appendFolded(nil, []byte(s)))
	switch {
	case strings.HasPrefix(f, "ext:"):
		var exts []string
		for e := range strings.SplitSeq(f[len("ext:"):], ";") {
			if e = strings.TrimPrefix(e, "."); e != "" {
				exts = append(exts, e)
			}
		}
		if len(exts) == 0 {
			return nil
		}
		return []term{{kind: termExt, not: not, exts: exts}}
	case strings.HasPrefix(f, "file:"), strings.HasPrefix(f, "folder:"):
		kind, rest := termFile, f[len("file:"):]
		if strings.HasPrefix(f, "folder:") {
			kind, rest = termFolder, f[len("folder:"):]
		}
		out := []term{{kind: kind, not: not}}
		if rest != "" {
			out = append(out, textTerm(rest, not))
		}
		return out
	case f == "":
		return nil
	}
	return []term{textTerm(f, not)}
}

func textTerm(f string, not bool) term {
	wild := strings.ContainsAny(f, "*?")
	path := strings.Contains(f, `\`)
	kind := termName
	switch {
	case wild && path:
		kind = termWildPath
	case wild:
		kind = termWildName
	case path:
		kind = termPath
	}
	return term{kind: kind, not: not, text: f}
}

// candidate is an entry being tested against a query; it builds the
// entry's folded path once, when a term needs it.
type candidate struct {
	v      *volume
	i      int
	path   string
	pathOK bool
	built  bool
}

func (c *candidate) foldedPath() (string, bool) {
	if !c.built {
		c.path, c.pathOK = c.v.path(c.i, true)
		c.built = true
	}
	return c.path, c.pathOK
}

func (q *query) match(c *candidate) bool {
	for _, g := range q.groups {
		ok := false
		for _, t := range g {
			if t.match(c) != t.not {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func (t *term) match(c *candidate) bool {
	dir := c.v.flags[c.i]&flagDir != 0
	switch t.kind {
	case termName:
		return bytes.Contains(c.v.name(c.i, true), []byte(t.text))
	case termWildName:
		return wildcard(t.text, string(c.v.name(c.i, true)))
	case termPath, termWildPath:
		p, ok := c.foldedPath()
		if !ok {
			return false
		}
		if t.kind == termPath {
			return strings.Contains(p, t.text)
		}
		return wildcard(t.text, p)
	case termExt:
		if dir {
			return false
		}
		name := c.v.name(c.i, true)
		dot := bytes.LastIndexByte(name, '.')
		if dot < 0 {
			return false
		}
		for _, e := range t.exts {
			if string(name[dot+1:]) == e {
				return true
			}
		}
		return false
	case termFile:
		return !dir
	case termFolder:
		return dir
	}
	return false
}

// wildcard matches s against a pattern of * (any run) and ? (any one
// character) as a whole.
func wildcard(pattern, s string) bool {
	p, r := []rune(pattern), []rune(s)
	pi, si := 0, 0
	star, mark := -1, 0
	for si < len(r) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == r[si]):
			pi++
			si++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// score ranks a matching entry: a name equal to the rank word first, then
// one starting with it, then one containing it; among those, entries
// nearer the root, shorter names and entries not hidden first.
func (q *query) score(v *volume, i, depth int) int {
	name := v.name(i, true)
	tier := 0
	if q.rank != "" {
		r := []byte(q.rank)
		switch {
		case bytes.Equal(name, r):
			tier = 3
		case bytes.HasPrefix(name, r):
			tier = 2
		case bytes.Contains(name, r):
			tier = 1
		}
	}
	s := tier<<24 - min(depth, 255)<<12 - min(len(name), 4095)
	if v.flags[i]&(flagHidden|flagSystem) != 0 {
		s -= 1 << 23
	}
	return s
}
