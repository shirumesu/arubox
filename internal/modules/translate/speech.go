package translate

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

// The Read Aloud service of the Edge browser. It is not an official API:
// the token, headers and framing follow the open-source edge-tts project and
// change from time to time.
const (
	// edgeClientToken is public: every Edge browser carries it.
	edgeClientToken = "6A5AA1D4EAFF4E9FB37E23D68491D6F4" //nolint:gosec // not a secret
	edgeSpeechURL   = "wss://speech.platform.bing.com/consumer/speech/synthesize/readaloud/edge/v1"
	edgeChromium    = "143.0.3650.75"
	// speechChunkSize is the most text, escaped, the service takes in one
	// request.
	speechChunkSize = 4096
	// speechTimeout bounds the reading of one chunk, so a silent connection
	// cannot leave the button speaking for good.
	speechTimeout = 30 * time.Second
)

// edgeVoices names a neural voice for each language the module offers, by
// lowercase language code.
var edgeVoices = map[string]string{
	"zh-cn": "zh-CN-XiaoxiaoNeural",
	"zh-tw": "zh-TW-HsiaoChenNeural",
	"en":    "en-US-AriaNeural",
	"ja":    "ja-JP-NanamiNeural",
	"ko":    "ko-KR-SunHiNeural",
	"fr":    "fr-FR-DeniseNeural",
	"de":    "de-DE-KatjaNeural",
	"es":    "es-ES-ElviraNeural",
	"it":    "it-IT-ElsaNeural",
	"pt":    "pt-BR-FranciscaNeural",
	"ru":    "ru-RU-SvetlanaNeural",
	"uk":    "uk-UA-PolinaNeural",
	"pl":    "pl-PL-ZofiaNeural",
	"nl":    "nl-NL-ColetteNeural",
	"tr":    "tr-TR-EmelNeural",
	"ar":    "ar-SA-ZariyahNeural",
	"th":    "th-TH-PremwadeeNeural",
	"vi":    "vi-VN-HoaiMyNeural",
	"id":    "id-ID-GadisNeural",
	"ms":    "ms-MY-YasminNeural",
}

// edgeVoice picks the voice for a language code such as "en" or "zh-CN". A
// bare "zh" is what some engines detect, and means simplified Chinese.
func edgeVoice(lang string) (string, bool) {
	code := strings.ToLower(lang)
	if code == "zh" {
		code = "zh-cn"
	}
	if v, ok := edgeVoices[code]; ok {
		return v, true
	}
	base, _, _ := strings.Cut(code, "-")
	v, ok := edgeVoices[base]
	return v, ok
}

// speak synthesizes text in the language's voice and returns it as MP3.
func (m *Module) speak(ctx context.Context, text, lang string) ([]byte, error) {
	voice, ok := edgeVoice(lang)
	if !ok {
		return nil, errors.New("这种语言不支持 Edge 在线朗读")
	}
	var audio bytes.Buffer
	for _, chunk := range splitSpeech(text) {
		if err := m.speakChunk(ctx, &audio, voice, chunk); err != nil {
			return nil, err
		}
	}
	if audio.Len() == 0 {
		return nil, errors.New("没有可以朗读的内容")
	}
	return audio.Bytes(), nil
}

// speakChunk appends the audio for one chunk of text. The service reads one
// request per connection.
func (m *Module) speakChunk(ctx context.Context, audio *bytes.Buffer, voice, text string) error {
	ctx, cancel := context.WithTimeout(ctx, speechTimeout)
	defer cancel()
	// fail names what went wrong, unless the user stopped the reading.
	fail := func(msg string) error {
		if errors.Is(ctx.Err(), context.Canceled) {
			return context.Canceled
		}
		return errors.New(msg)
	}

	endpoint := fmt.Sprintf("%s?TrustedClientToken=%s&ConnectionId=%s&Sec-MS-GEC=%s&Sec-MS-GEC-Version=2-%s",
		edgeSpeechURL, edgeClientToken, randomHex(16), secMSGEC(time.Now()), edgeChromium)
	major, _, _ := strings.Cut(edgeChromium, ".")
	header := http.Header{
		"User-Agent": {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + major + ".0.0.0 Safari/537.36 Edg/" + major + ".0.0.0"},
		"Origin":     {"chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold"},
		"Pragma":     {"no-cache"},
		"Cookie":     {"muid=" + strings.ToUpper(randomHex(16)) + ";"},
	}
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: m.client, HTTPHeader: header}) //nolint:bodyclose // closed by the library
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			return errors.New("请求被 Edge 在线朗读拒绝，请检查系统时间是否准确")
		}
		return fail("无法连接 Edge 在线朗读，请检查网络或代理")
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(1 << 20)

	config := "X-Timestamp:" + javascriptDate() + "\r\n" +
		"Content-Type:application/json; charset=utf-8\r\n" +
		"Path:speech.config\r\n\r\n" +
		`{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"false"},"outputFormat":"audio-24khz-48kbitrate-mono-mp3"}}}}` + "\r\n"
	// The Z after the date is an Edge quirk the service expects.
	ssml := "X-RequestId:" + randomHex(16) + "\r\n" +
		"Content-Type:application/ssml+xml\r\n" +
		"X-Timestamp:" + javascriptDate() + "Z\r\n" +
		"Path:ssml\r\n\r\n" +
		"<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='en-US'>" +
		"<voice name='" + voice + "'><prosody pitch='+0Hz' rate='+0%' volume='+0%'>" +
		speechMarkup(text) +
		"</prosody></voice></speak>"
	for _, msg := range []string{config, ssml} {
		if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
			return fail("向 Edge 在线朗读发送请求失败")
		}
	}

	received := false
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return fail("读取 Edge 在线朗读的结果时连接中断")
		}
		if kind == websocket.MessageText {
			// Text frames carry progress; the turn ends the reading.
			headers, _, _ := strings.Cut(string(data), "\r\n\r\n")
			if frameHeader(headers, "Path") != "turn.end" {
				continue
			}
			if !received {
				return errors.New("没有收到 Edge 在线朗读的声音")
			}
			return nil
		}
		// Binary frames start with the length of their headers, then the
		// headers, then the audio.
		if len(data) < 2 || int(binary.BigEndian.Uint16(data))+2 > len(data) {
			return errors.New("无法识别 Edge 在线朗读返回的内容")
		}
		end := 2 + int(binary.BigEndian.Uint16(data))
		if frameHeader(string(data[2:end]), "Path") != "audio" {
			return errors.New("无法识别 Edge 在线朗读返回的内容")
		}
		if len(data) > end {
			audio.Write(data[end:])
			received = true
		}
	}
}

// frameHeader reads one header of a service frame, or "" when it is absent.
func frameHeader(headers, name string) string {
	for _, line := range strings.Split(headers, "\r\n") {
		if key, value, ok := strings.Cut(line, ":"); ok && key == name {
			return value
		}
	}
	return ""
}

// javascriptDate formats the time the way the service's JavaScript clients
// do.
func javascriptDate() string {
	return time.Now().UTC().Format("Mon Jan 02 2006 15:04:05") + " GMT+0000 (Coordinated Universal Time)"
}

// randomHex returns n random bytes as hex, which is what the service takes
// as an identifier. crypto/rand does not fail.
func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// speechMarkup makes text safe to place in SSML. The service refuses control
// characters other than tab, line feed and carriage return.
func speechMarkup(text string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return ' '
		}
		return r
	}, text)
	return xmlEscaper.Replace(clean)
}

// splitSpeech cuts text into pieces the service takes in one request,
// preferably at the end of a sentence or line, else at a space.
func splitSpeech(text string) []string {
	var chunks []string
	rest := text
	for len(rest) > 0 {
		n, size := 0, 0
		for _, r := range rest {
			cost := len(speechMarkup(string(r)))
			if size+cost > speechChunkSize {
				break
			}
			size += cost
			n += utf8.RuneLen(r)
		}
		if n < len(rest) {
			if b := strings.LastIndexAny(rest[:n], "\n。！？.!? "); b >= 0 {
				_, width := utf8.DecodeRuneInString(rest[b:])
				n = b + width
			}
		}
		if strings.TrimSpace(rest[:n]) != "" {
			chunks = append(chunks, rest[:n])
		}
		rest = rest[n:]
	}
	return chunks
}
