package launcher

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// Keyword is a word that, typed before a query and a space, searches a
// website for the query.
type Keyword struct {
	Keyword string `json:"keyword"`
	Name    string `json:"name"`
	// URL is the search address, with {query} where the query goes.
	URL string `json:"url"`
}

// queryMark is where a keyword's URL takes the query.
const queryMark = "{query}"

var defaultKeywords = []Keyword{
	{Keyword: "gg", Name: "Google", URL: "https://www.google.com/search?q={query}"},
	{Keyword: "gh", Name: "GitHub", URL: "https://github.com/search?q={query}&type=repositories"},
}

// searchURL is the address that searches for query, or the site's home page
// when the query is empty.
func (k Keyword) searchURL(query string) string {
	if strings.TrimSpace(query) == "" {
		if u, err := url.Parse(k.URL); err == nil {
			return u.Scheme + "://" + u.Host
		}
	}
	// QueryEscape writes spaces as +, which is a plus in a path; %20 is a
	// space anywhere.
	q := strings.ReplaceAll(url.QueryEscape(query), "+", "%20")
	return strings.ReplaceAll(k.URL, queryMark, q)
}

// checkKeywords reports what is wrong with the keywords, if anything: each
// is one word, used once, and searches an http or https address.
func checkKeywords(s Settings) error {
	seen := map[string]bool{}
	use := func(word, what string) error {
		w := strings.ToLower(word)
		switch {
		case w == "":
			return errors.New(what + "的关键词不能为空")
		case strings.ContainsFunc(w, unicode.IsSpace):
			return errors.New("关键词“" + word + "”不能包含空格")
		case strings.HasPrefix(w, "!"):
			return errors.New("关键词不能以 ! 开头，它用来运行命令")
		case seen[w]:
			return errors.New("关键词“" + word + "”重复了")
		}
		seen[w] = true
		return nil
	}
	for _, b := range []struct{ word, what string }{
		{s.TranslateKeyword, "翻译"},
		{s.FilesKeyword, "搜索文件"},
	} {
		if b.word == "" {
			continue // off
		}
		if err := use(b.word, b.what); err != nil {
			return err
		}
	}
	defaultFound := s.DefaultSearch == ""
	for _, k := range s.Keywords {
		if strings.TrimSpace(k.Name) == "" {
			return errors.New("关键词“" + k.Keyword + "”缺少名称")
		}
		if err := use(k.Keyword, k.Name); err != nil {
			return err
		}
		u, err := url.Parse(strings.ReplaceAll(k.URL, queryMark, "q"))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New(k.Name + "的网址需要以 http:// 或 https:// 开头")
		}
		if !strings.Contains(k.URL, queryMark) {
			return errors.New(k.Name + "的网址里需要用 " + queryMark + " 标出搜索内容的位置")
		}
		defaultFound = defaultFound || strings.EqualFold(k.Keyword, s.DefaultSearch)
	}
	if !defaultFound {
		return errors.New("默认搜索引用了不存在的关键词")
	}
	return nil
}
