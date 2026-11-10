package launcher

import (
	_ "embed"
	"strings"
	"unicode"
)

// pinyinTable holds the initial of the most common Mandarin reading of each
// character from U+3400 to U+9FFF, or a space for none. It is derived from
// the kMandarin field of the Unicode Character Database's Unihan files
// (Unihan_Readings.txt), © Unicode, Inc., under the Unicode License v3.
//
//go:embed pinyin.txt
var pinyinTable string

const pinyinFirst = 0x3400

// pinyinInitials spells a name by the pinyin initials of its Chinese
// characters, keeping its Latin letters and digits: 微信 is "wx" and QQ音乐
// is "qqyl". It returns "" for names without Chinese characters.
func pinyinInitials(name string) string {
	var b strings.Builder
	han := false
	for _, r := range name {
		switch {
		case r >= pinyinFirst && int(r-pinyinFirst) < len(pinyinTable) && pinyinTable[r-pinyinFirst] != ' ':
			b.WriteByte(pinyinTable[r-pinyinFirst])
			han = true
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	if !han {
		return ""
	}
	return b.String()
}
