//go:build windows

package fileindex

import (
	"bytes"
	"strings"
	"unicode/utf8"
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
	// only is termFile or termFolder for a word restricted to files or to
	// folders, as in "file:report", where not negates both together.
	only termKind
	text string   // folded
	exts []string // folded, for termExt
	wild []rune   // text as characters, for termWildName and termWildPath
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
			if t, ok := parseTerm(alt); ok {
				ts = append(ts, t)
			}
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
		q.groups = append(q.groups, ts)
	}
	for _, g := range q.groups {
		if len(g) != 1 || g[0].not {
			continue
		}
		t := g[0]
		anchor := ""
		switch t.kind {
		case termName:
			anchor = t.text
		case termWildName:
			anchor = literal(t.text)
		}
		if len(anchor) > len(q.anchor) {
			q.anchor = anchor
		}
		if q.rank == "" && (t.kind == termName || t.kind == termPath) {
			q.rank = t.text[strings.LastIndexByte(t.text, '\\')+1:]
		}
	}
	return q
}

// literal returns the longest run of a wildcard pattern without * or ?,
// which every name the pattern matches contains.
func literal(pattern string) string {
	best := ""
	for part := range strings.FieldsFuncSeq(pattern, func(r rune) bool { return r == '*' || r == '?' }) {
		if len(part) > len(best) {
			best = part
		}
	}
	return best
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

// parseTerm parses one word of a query; ok is false when it asks for
// nothing, such as an empty "ext:".
func parseTerm(s string) (t term, ok bool) {
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
			return term{}, false
		}
		return term{kind: termExt, not: not, exts: exts}, true
	case strings.HasPrefix(f, "file:"), strings.HasPrefix(f, "folder:"):
		kind, rest := termFile, f[len("file:"):]
		if strings.HasPrefix(f, "folder:") {
			kind, rest = termFolder, f[len("folder:"):]
		}
		if rest == "" {
			return term{kind: kind, not: not}, true
		}
		t := textTerm(rest, not)
		t.only = kind
		return t, true
	case f == "":
		return term{}, false
	}
	return textTerm(f, not), true
}

func textTerm(f string, not bool) term {
	wild := strings.ContainsAny(f, "*?")
	path := strings.Contains(f, `\`)
	t := term{kind: termName, not: not, text: f}
	switch {
	case wild && path:
		t.kind = termWildPath
	case wild:
		t.kind = termWildName
	case path:
		t.kind = termPath
	}
	if wild {
		t.wild = []rune(f)
	}
	return t
}

// candidate is an entry being tested against a query; it builds the
// entry's folded path once, when a term needs it, in a buffer that the
// next entry reuses.
type candidate struct {
	v      *volume
	i      int
	path   []byte
	pathOK bool
	built  bool
}

func (c *candidate) foldedPath() ([]byte, bool) {
	if !c.built {
		c.path, c.pathOK = c.v.appendPath(c.path[:0], c.i, true)
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
	if t.only == termFile && dir || t.only == termFolder && !dir {
		return false
	}
	switch t.kind {
	case termName:
		return bytes.Contains(c.v.name(c.i, true), []byte(t.text))
	case termWildName:
		return wildcard(t.wild, c.v.name(c.i, true))
	case termPath, termWildPath:
		p, ok := c.foldedPath()
		if !ok {
			return false
		}
		if t.kind == termPath {
			return bytes.Contains(p, []byte(t.text))
		}
		return wildcard(t.wild, p)
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

// wildcard matches s, UTF-8, against a pattern of * (any run) and ? (any
// one character) as a whole.
func wildcard(p []rune, s []byte) bool {
	pi, si := 0, 0
	star, mark := -1, 0
	for si < len(s) {
		r, n := utf8.DecodeRune(s[si:])
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == r):
			pi++
			si += n
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			pi = star + 1
			_, m := utf8.DecodeRune(s[mark:])
			mark += m
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
