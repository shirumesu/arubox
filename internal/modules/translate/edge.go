package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// edgeEndpoint is the key-less endpoint the Edge browser's page translation
// uses. It is not an official API and may be rate limited or change; it
// replaced the older one that needed a token from /translate/auth.
const edgeEndpoint = "https://edge.microsoft.com/translate/translatetext"

// edgeTranslator is Microsoft's translator, reached the way Edge does.
type edgeTranslator struct {
	client *http.Client
}

func (e *edgeTranslator) translate(ctx context.Context, text, source, target string) (Translation, error) {
	from := ""
	if source != autoDetect {
		from = edgeLanguage(source)
	}
	query := url.Values{"from": {from}, "to": {edgeLanguage(target)}, "isEnterpriseClient": {"false"}}
	payload, err := json.Marshal([]string{text})
	if err != nil {
		return Translation{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, edgeEndpoint+"?"+query.Encode(), bytes.NewReader(payload))
	if err != nil {
		return Translation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Translation{}, err
		}
		return Translation{}, errors.New("无法连接 Edge 翻译，请检查网络或代理")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Translation{}, errors.New("读取 Edge 翻译的结果时连接中断")
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest:
		return Translation{}, errors.New("这种语言不支持 Edge 翻译")
	case http.StatusTooManyRequests:
		return Translation{}, errors.New("请求过于频繁，Edge 翻译暂时限制了访问，请稍后再试")
	default:
		return Translation{}, fmt.Errorf("调用 Edge 翻译出错（HTTP %d）", resp.StatusCode)
	}
	result, err := parseEdge(body)
	if err != nil {
		return Translation{}, err
	}
	if source != autoDetect {
		result.Source = source
	}
	result.Target = target
	return result, nil
}

// parseEdge reads the answer, which has one entry per text sent. The
// detected language is only present when the source was not given.
func parseEdge(body []byte) (Translation, error) {
	var r []struct {
		DetectedLanguage struct {
			Language string `json:"language"`
		} `json:"detectedLanguage"`
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Translation{}, errors.New("无法识别 Edge 翻译返回的内容")
	}
	if len(r) == 0 || len(r[0].Translations) == 0 || r[0].Translations[0].Text == "" {
		return Translation{}, errors.New("没有收到 Edge 翻译的译文")
	}
	return Translation{Text: r[0].Translations[0].Text, Source: fromEdge(r[0].DetectedLanguage.Language)}, nil
}

// edgeLanguage turns a Google language code into a Microsoft one. Only
// Chinese differs, by script instead of region.
func edgeLanguage(code string) string {
	switch strings.ToLower(code) {
	case "zh-cn":
		return "zh-Hans"
	case "zh-tw":
		return "zh-Hant"
	}
	return code
}

// fromEdge turns a detected Microsoft language into a Google language code.
func fromEdge(code string) string {
	switch strings.ToLower(code) {
	case "zh-hans":
		return "zh-CN"
	case "zh-hant":
		return "zh-TW"
	}
	return code
}
