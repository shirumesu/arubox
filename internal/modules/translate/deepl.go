package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DeepL serves free keys, which end in ":fx", from its own host.
const (
	deeplEndpoint     = "https://api.deepl.com/v2/translate"
	deeplFreeEndpoint = "https://api-free.deepl.com/v2/translate"
)

// deepl is the official DeepL API, used with the user's key.
type deepl struct {
	client *http.Client
	key    string
}

func (d *deepl) translate(ctx context.Context, text, source, target string) (Translation, error) {
	endpoint := deeplEndpoint
	if strings.HasSuffix(d.key, ":fx") {
		endpoint = deeplFreeEndpoint
	}
	payload := struct {
		Text       []string `json:"text"`
		TargetLang string   `json:"target_lang"`
		SourceLang string   `json:"source_lang,omitempty"`
	}{Text: []string{text}, TargetLang: deeplTarget(target)}
	if source != autoDetect {
		payload.SourceLang = deeplSource(source)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Translation{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Translation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "DeepL-Auth-Key "+strings.TrimSpace(d.key))
	resp, err := d.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Translation{}, err
		}
		return Translation{}, errors.New("无法连接 DeepL，请检查网络或代理")
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Translation{}, errors.New("读取 DeepL 的结果时连接中断")
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return Translation{}, errors.New("DeepL 拒绝了 API 密钥，请在设置里检查密钥")
	case 456:
		return Translation{}, errors.New("本月的 DeepL 额度已经用完")
	case http.StatusTooManyRequests:
		return Translation{}, errors.New("请求过于频繁，DeepL 暂时限制了访问，请稍后再试")
	case http.StatusBadRequest:
		return Translation{}, errors.New("DeepL 不支持这种语言")
	default:
		return Translation{}, fmt.Errorf("调用 DeepL 出错（HTTP %d）", resp.StatusCode)
	}
	result, err := parseDeepL(body)
	if err != nil {
		return Translation{}, err
	}
	if source != autoDetect {
		result.Source = source
	}
	result.Target = target
	return result, nil
}

func parseDeepL(body []byte) (Translation, error) {
	var r struct {
		Translations []struct {
			DetectedSourceLanguage string `json:"detected_source_language"`
			Text                   string `json:"text"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Translation{}, errors.New("无法识别 DeepL 返回的内容")
	}
	if len(r.Translations) == 0 || r.Translations[0].Text == "" {
		return Translation{}, errors.New("没有收到 DeepL 的译文")
	}
	t := r.Translations[0]
	return Translation{Text: t.Text, Source: fromDeepL(t.DetectedSourceLanguage)}, nil
}

// deeplTarget turns a Google language code into a DeepL target language.
// DeepL wants the variant for Chinese, English and Portuguese.
func deeplTarget(code string) string {
	switch strings.ToLower(code) {
	case "zh-cn":
		return "ZH-HANS"
	case "zh-tw":
		return "ZH-HANT"
	case "en":
		return "EN-US"
	case "pt":
		return "PT-BR"
	}
	return strings.ToUpper(code)
}

// deeplSource turns a Google language code into a DeepL source language,
// which never names a variant.
func deeplSource(code string) string {
	base, _, _ := strings.Cut(code, "-")
	return strings.ToUpper(base)
}

// fromDeepL turns a detected DeepL language into a Google language code.
// DeepL does not tell simplified and traditional Chinese apart.
func fromDeepL(code string) string {
	if strings.EqualFold(code, "zh") {
		return "zh-CN"
	}
	return strings.ToLower(code)
}
