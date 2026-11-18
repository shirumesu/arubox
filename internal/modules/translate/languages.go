package translate

import "strings"

// language is one language offered as a source or target, by Google
// language code.
type language struct {
	code, name string
}

// languages are the languages the pickers offer.
var languages = []language{
	{"zh-CN", "简体中文"},
	{"zh-TW", "繁体中文"},
	{"en", "英语"},
	{"ja", "日语"},
	{"ko", "韩语"},
	{"fr", "法语"},
	{"de", "德语"},
	{"es", "西班牙语"},
	{"it", "意大利语"},
	{"pt", "葡萄牙语"},
	{"ru", "俄语"},
	{"uk", "乌克兰语"},
	{"pl", "波兰语"},
	{"nl", "荷兰语"},
	{"tr", "土耳其语"},
	{"ar", "阿拉伯语"},
	{"th", "泰语"},
	{"vi", "越南语"},
	{"id", "印尼语"},
	{"ms", "马来语"},
}

// languageName names a language code, including detected ones outside the
// list.
func languageName(code string) string {
	for _, l := range languages {
		if strings.EqualFold(l.code, code) {
			return l.name
		}
	}
	if strings.EqualFold(code, "zh") {
		return "中文"
	}
	return strings.ToUpper(code)
}

// option is one entry of a picker: a stored value and what it shows.
type option struct {
	value, label string
}

// languageOptions lists the languages for a picker showing value. A value
// outside the list, such as a detected code, is offered first so the
// picker can show it; withAuto adds automatic detection on top, named
// after the detected language when there is one.
func languageOptions(value string, withAuto bool, detected string) []option {
	opts := make([]option, 0, len(languages)+2)
	if withAuto {
		label := "自动检测"
		if detected != "" {
			label = "自动检测（" + languageName(detected) + "）"
		}
		opts = append(opts, option{autoDetect, label})
	}
	known := value == autoDetect
	for _, l := range languages {
		if strings.EqualFold(l.code, value) {
			known = true
		}
	}
	if !known {
		opts = append(opts, option{value, languageName(value)})
	}
	for _, l := range languages {
		opts = append(opts, option{l.code, l.name})
	}
	return opts
}

// engineOptions lists the engines; DeepL only once it has a key.
func engineOptions(s Settings) []option {
	opts := []option{{engineGoogle, "Google 翻译"}, {engineEdge, "Edge 翻译"}}
	if strings.TrimSpace(s.DeepLKey) != "" || s.Engine == engineDeepL {
		opts = append(opts, option{engineDeepL, "DeepL"})
	}
	return opts
}

// voiceOptions lists where read aloud can get its voice.
var voiceOptions = []option{{voiceSystem, "系统声音"}, {voiceEdge, "Edge 在线朗读"}}

// optionIndex finds value among opts, or -1.
func optionIndex(opts []option, value string) int {
	for i, o := range opts {
		if strings.EqualFold(o.value, value) {
			return i
		}
	}
	return -1
}

// optionLabels returns what the options show.
func optionLabels(opts []option) []string {
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.label
	}
	return labels
}
