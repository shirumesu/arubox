// Package translate is the translation module: input, selection and
// screenshot translation and reading aloud, all in one floating popup.
package translate

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
)

const id = "translate"

// Store keeps the module's settings. config.Store satisfies it.
type Store interface {
	ModuleSettings(id string, v any) error
	SetModuleSettings(id string, v any) error
}

// TextReader reads the text in a picture with the OCR engine the user
// chose. The core provides it.
type TextReader interface {
	ImageText(img platform.Image) ([]platform.TextLine, error)
}

// Module is the translation module.
type Module struct {
	store  Store
	reader TextReader
	log    *slog.Logger
	client *http.Client
	google *google
	edge   *edgeTranslator
	page   *settingsPage

	mu       sync.Mutex
	settings Settings
	enabled  bool

	// requests numbers what the popup is asked to show. A selection or a
	// picture's text that takes a while to read shows only if nothing was
	// asked for since, so it does not wipe out what the user is typing.
	requests atomic.Uint64
	// capturing is set from when the screen starts being taken until its
	// window is up, so that a second press does not take it again.
	capturing atomic.Bool

	// The windows belong to the UI thread and are only touched there. They
	// exist while shown: native windows are cheap to make, so nothing is
	// kept ready while hidden.
	popup   *popup
	capture *capture
}

// New creates the module. It does nothing until enabled.
func New(store Store, log *slog.Logger, reader TextReader) *Module {
	settings := defaultSettings()
	if err := store.ModuleSettings(id, &settings); err != nil {
		log.Warn("translate settings unreadable, using defaults", "error", err)
		settings = defaultSettings()
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		// Connections are kept for the next translation of a session, not
		// for good.
		Transport: &http.Transport{Proxy: platform.SystemProxy, ForceAttemptHTTP2: true, IdleConnTimeout: 90 * time.Second},
	}
	m := &Module{
		store:    store,
		reader:   reader,
		log:      log,
		settings: settings,
		client:   client,
		google:   &google{client: client},
		edge:     &edgeTranslator{client: client},
	}
	m.popup = &popup{m: m}
	m.page = &settingsPage{m: m}
	return m
}

// Info describes the module.
func (m *Module) Info() module.Info {
	return module.Info{
		ID:          id,
		Name:        "翻译",
		Description: "输入翻译、划词翻译、截图翻译和朗读，都在同一个浮窗里完成。",
		Icon:        ui.IconTranslate,
	}
}

// Hotkeys declares the hotkeys. Alt with a letter is taken by menus and
// browsers, so the defaults add Shift.
func (m *Module) Hotkeys() []hotkey.Def {
	return []hotkey.Def{
		{ID: "input", Label: "输入翻译", Default: "Alt+Shift+a", Action: m.translateInput},
		{ID: "selection", Label: "划词翻译", Default: "Alt+Shift+d", Action: m.translateSelection},
		{ID: "screen", Label: "截图翻译", Default: "Alt+Shift+s", Action: m.translateScreen},
	}
}

// Enable turns the module on. The windows are made when first shown.
func (m *Module) Enable(module.Context) error {
	m.mu.Lock()
	m.enabled = true
	m.mu.Unlock()
	return nil
}

// Disable closes the windows and stops any reading. It runs on the UI
// thread.
func (m *Module) Disable() error {
	m.mu.Lock()
	m.enabled = false
	m.mu.Unlock()
	m.popup.hide()
	if m.capture != nil {
		m.capture.close()
	}
	return nil
}

// Settings returns the dashboard page.
func (m *Module) Settings() module.Page { return m.page }

func (m *Module) isEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// open shows req in the popup, from any goroutine, unless the popup has
// been asked for something else since seq, from m.requests, was taken.
func (m *Module) open(seq uint64, req request) {
	ui.Post(func() {
		if m.isEnabled() && m.requests.Load() == seq {
			m.popup.open(req)
		}
	})
}

func (m *Module) translateInput() {
	m.open(m.requests.Add(1), request{})
}

// translateSelection reads the selection on the hotkey's goroutine, since
// it may simulate a copy and wait for it.
func (m *Module) translateSelection() {
	seq := m.requests.Add(1)
	text, err := platform.SelectedText()
	switch {
	case err != nil:
		m.log.Warn("read selection", "error", err)
		m.open(seq, request{notice: "读取选中的文字失败，可以直接输入要翻译的内容。"})
	case strings.TrimSpace(text) == "":
		m.open(seq, request{notice: "没有取到选中的文字，可以直接输入要翻译的内容。"})
	default:
		m.open(seq, request{text: text})
	}
}

// translateScreen freezes the display under the mouse and lets the user
// drag out an area whose text is then translated.
func (m *Module) translateScreen() {
	if !m.isEnabled() || !m.capturing.CompareAndSwap(false, true) {
		return
	}
	var busy bool
	ui.Sync(func() { busy = m.capture != nil })
	if busy {
		m.capturing.Store(false)
		return
	}
	seq := m.requests.Add(1)
	mon := ui.MonitorAt(ui.CursorPos())
	b := mon.Bounds
	img, err := platform.CaptureScreen(platform.Rect{X: b.X, Y: b.Y, Width: b.W, Height: b.H})
	if err != nil {
		m.capturing.Store(false)
		m.log.Warn("capture screen", "error", err)
		m.open(seq, request{notice: err.Error() + "，可以直接输入要翻译的内容。"})
		return
	}
	ui.Post(func() {
		defer m.capturing.Store(false)
		if m.capture != nil || !m.isEnabled() {
			return
		}
		c, err := newCapture(m, b, img)
		if err != nil {
			m.log.Warn("show capture", "error", err)
			return
		}
		m.capture = c
	})
}

// readScreen reads the text in img, the area the user dragged out, and
// translates it. It runs on its own goroutine.
func (m *Module) readScreen(seq uint64, img platform.Image) {
	lines, err := m.reader.ImageText(img)
	text := platform.Paragraphs(lines)
	switch {
	case err != nil:
		m.log.Warn("read screen", "error", err)
		m.open(seq, request{notice: err.Error() + "，可以直接输入要翻译的内容。"})
	case strings.TrimSpace(text) == "":
		m.open(seq, request{notice: "没有在选中的区域里识别到文字。"})
	default:
		m.open(seq, request{text: text})
	}
}

func (m *Module) currentSettings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

func (m *Module) setSettings(s Settings) error {
	if err := s.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.SetModuleSettings(id, s); err != nil {
		return errors.New("保存设置失败：" + err.Error())
	}
	m.settings = s
	return nil
}

// ErrDisabled is returned to other modules while translation is turned off.
var ErrDisabled = errors.New("翻译未开启")

// TranslateText translates text as the popup would, for other modules, and
// names the engine that did it. It returns ErrDisabled while the module is
// off.
func (m *Module) TranslateText(ctx context.Context, text string) (Translation, string, error) {
	if !m.isEnabled() {
		return Translation{}, "", ErrDisabled
	}
	t, err := m.translate(ctx, strings.TrimSpace(text))
	return t, engineName(m.currentSettings().Engine), err
}

// engineName is the name an engine is shown under.
func engineName(engine string) string {
	switch engine {
	case engineDeepL:
		return "DeepL"
	case engineEdge:
		return "Edge 翻译"
	}
	return "Google 翻译"
}

// Available reports whether the module is on, for other modules.
func (m *Module) Available() bool {
	return m.isEnabled()
}

// OpenText shows text, translated, in the popup. It is safe from any
// goroutine.
func (m *Module) OpenText(text string) {
	m.open(m.requests.Add(1), request{text: text})
}

// engine translates text from one language into another.
type engine interface {
	translate(ctx context.Context, text, source, target string) (Translation, error)
}

// translate translates text with the current engine and languages. When the
// source language is detected and turns out to be the target one, it
// translates into the second language instead.
func (m *Module) translate(ctx context.Context, text string) (Translation, error) {
	s := m.currentSettings()
	var e engine = m.google
	switch s.Engine {
	case engineDeepL:
		e = &deepl{client: m.client, key: s.DeepLKey}
	case engineEdge:
		e = m.edge
	}
	t, err := e.translate(ctx, text, s.Source, s.Target)
	if err != nil || s.Source != autoDetect || !sameLanguage(t.Source, s.Target) || sameLanguage(s.Second, s.Target) {
		return t, err
	}
	return e.translate(ctx, text, s.Source, s.Second)
}
