package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// googleEndpoint is the key-less endpoint Google's own lightweight clients
// use. It is not an official API and may be rate limited or change.
const googleEndpoint = "https://translate.googleapis.com/translate_a/single"

// Translation is one engine's answer.
type Translation struct {
	Text string `json:"text"`
	// Source is the language of the text: the one asked for, or the one
	// Google detected.
	Source string `json:"source"`
	Target string `json:"target"`
}

type google struct {
	client *http.Client
}

func (g *google) translate(ctx context.Context, text, source, target string) (Translation, error) {
	query := url.Values{"client": {"gtx"}, "sl": {source}, "tl": {target}, "dt": {"t"}, "dj": {"1"}}
	// The text travels in the body: a URL is too short for long passages.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleEndpoint+"?"+query.Encode(),
		strings.NewReader(url.Values{"q": {text}}.Encode()))
	if err != nil {
		return Translation{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	resp, err := g.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Translation{}, err
		}
		return Translation{}, errors.New("无法连接 Google 翻译，请检查网络或代理")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Translation{}, errors.New("读取 Google 翻译的结果时连接中断")
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return Translation{}, errors.New("请求过于频繁，Google 翻译暂时限制了访问，请稍后再试")
	case resp.StatusCode != http.StatusOK:
		return Translation{}, fmt.Errorf("调用 Google 翻译出错（HTTP %d）", resp.StatusCode)
	}
	result, err := parseGoogle(body)
	if err != nil {
		return Translation{}, err
	}
	if source != autoDetect {
		result.Source = source
	}
	result.Target = target
	return result, nil
}

// parseGoogle reads the dj=1 response, which splits the text into sentences.
func parseGoogle(body []byte) (Translation, error) {
	var r struct {
		Sentences []struct {
			Trans *string `json:"trans"`
		} `json:"sentences"`
		Src string `json:"src"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Translation{}, errors.New("无法识别 Google 翻译返回的内容")
	}
	var b strings.Builder
	for _, s := range r.Sentences {
		// Sentences without trans carry transliteration only.
		if s.Trans != nil {
			b.WriteString(*s.Trans)
		}
	}
	if b.Len() == 0 {
		return Translation{}, errors.New("没有收到 Google 翻译的译文")
	}
	return Translation{Text: b.String(), Source: r.Src}, nil
}
