package platform

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Paragraphs joins lines read off the screen into the text to translate.
// Lines close below each other belong to one paragraph and are joined, so a
// sentence broken across lines is translated as one; a wider gap, or a line
// that does not follow below, starts a new paragraph.
func Paragraphs(lines []TextLine) string {
	var b strings.Builder
	var prev Box
	for _, line := range lines {
		text := tidyLine(line.Text)
		if text == "" {
			continue
		}
		box := line.Box
		if b.Len() > 0 {
			height := max(prev.Height, box.Height)
			gap := box.Y - (prev.Y + prev.Height)
			switch {
			case box.Y < prev.Y+prev.Height/2 || gap > 0.75*height:
				b.WriteString("\n")
			default:
				joined := b.String()
				last, _ := utf8.DecodeLastRuneInString(joined)
				first, _ := utf8.DecodeRuneInString(text)
				switch {
				case last == '-' && unicode.IsLower(first):
					// A word hyphenated across the line break.
					b.Reset()
					b.WriteString(strings.TrimSuffix(joined, "-"))
				case !isCJK(last) && !isCJK(first):
					b.WriteString(" ")
				}
			}
		}
		b.WriteString(text)
		prev = box
	}
	return b.String()
}

// tidyLine trims a line and removes the spaces some OCR engines put between
// Chinese, Japanese and Korean characters.
func tidyLine(s string) string {
	fields := strings.Fields(s)
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			last, _ := utf8.DecodeLastRuneInString(fields[i-1])
			first, _ := utf8.DecodeRuneInString(f)
			if !isCJK(last) || !isCJK(first) {
				b.WriteString(" ")
			}
		}
		b.WriteString(f)
	}
	return b.String()
}

// isCJK reports whether r is written without spaces between words.
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) ||
		(r >= 0x3000 && r <= 0x303f) || // CJK punctuation
		(r >= 0xff00 && r <= 0xffef) // full-width forms
}
