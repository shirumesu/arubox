// Package translate is the translation module: input, selection and
// screenshot translation and reading aloud, all in one floating popup.
package translate

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/platform"
)

const id = "translate"

// Store keeps the module's settings. config.Store satisfies it.
type Store interface {
	ModuleSettings(id string, v any) error
	SetModuleSettings(id string, v any) error
}

// Module is the translation module.
type Module struct {
	store  Store
	log    *slog.Logger
	client *http.Client
	google *google

	mu       sync.Mutex
	settings Settings
	popup    *popup   // nil while disabled
	capture  *capture // nil while disabled
}

// New creates the module. It does nothing until enabled.
func New(store Store, log *slog.Logger) *Module {
	settings := defaultSettings()
	if err := store.ModuleSettings(id, &settings); err != nil {
		log.Warn("translate settings unreadable, using defaults", "error", err)
		settings = defaultSettings()
	}
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: platform.SystemProxy, ForceAttemptHTTP2: true},
	}
	return &Module{
		store:    store,
		log:      log,
		settings: settings,
		client:   client,
		google:   &google{client: client},
	}
}

// Info describes the module.
func (m *Module) Info() module.Info {
	return module.Info{
		ID:          id,
		Name:        "翻译",
		Description: "输入翻译、划词翻译、截图翻译和朗读，都在同一个浮窗里完成。",
		Platforms:   []string{"darwin", "windows"},
	}
}

// Services binds the popup, the capture page and the settings page, and
// serves the frozen screen to the capture page.
func (m *Module) Services() []application.Service {
	return []application.Service{
		application.NewService(&Service{module: m}),
		application.NewServiceWithOptions(&frames{module: m}, application.ServiceOptions{Route: frameRoute}),
	}
}

// Hotkeys declares the hotkeys. On Windows, Alt with a letter is taken by
// menus and browsers, so the defaults add Shift there.
func (m *Module) Hotkeys() []hotkey.Def {
	input, selection, screen := "Alt+a", "Alt+d", "Alt+s"
	if runtime.GOOS == "windows" {
		input, selection, screen = "Alt+Shift+a", "Alt+Shift+d", "Alt+Shift+s"
	}
	return []hotkey.Def{
		{ID: "input", Label: "输入翻译", Default: input, Action: m.translateInput},
		{ID: "selection", Label: "划词翻译", Default: selection, Action: m.translateSelection},
		{ID: "screen", Label: "截图翻译", Default: screen, Action: m.translateScreen},
	}
}

// Enable loads the popup and the capture window in the background so they
// open without delay.
func (m *Module) Enable(ctx module.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.popup = newPopup(ctx.App, m.keepTime)
	c := &capture{app: ctx.App, keep: m.keepTime}
	c.mu.Lock()
	c.create()
	c.mu.Unlock()
	m.capture = c
	go platform.PrepareScreenCapture()
	return nil
}

// Disable closes the windows.
func (m *Module) Disable() error {
	m.mu.Lock()
	p, c := m.popup, m.capture
	m.popup, m.capture = nil, nil
	m.mu.Unlock()
	if p != nil {
		p.mu.Lock()
		p.destroy()
		p.mu.Unlock()
	}
	if c != nil {
		c.mu.Lock()
		c.destroy()
		c.mu.Unlock()
	}
	return nil
}

func (m *Module) keepTime() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Duration(m.settings.KeepMinutes) * time.Minute
}

// currentPopup returns the popup, or nil while the module is disabled.
func (m *Module) currentPopup() *popup {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.popup
}

// currentCapture returns the capture window, or nil while the module is
// disabled.
func (m *Module) currentCapture() *capture {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capture
}

func (m *Module) translateInput() {
	if p := m.currentPopup(); p != nil {
		p.open(Request{})
	}
}

func (m *Module) translateSelection() {
	p := m.currentPopup()
	if p == nil {
		return
	}
	text, err := platform.SelectedText()
	switch {
	case errors.Is(err, platform.ErrNoAccessibility):
		platform.RequestAccessibility()
		p.open(Request{Notice: "划词翻译需要辅助功能权限，用来读取其他程序里选中的文字。", Permission: permissionAccessibility})
	case err != nil:
		m.log.Warn("read selection", "error", err)
		p.open(Request{Notice: "读取选中的文字失败，可以直接输入要翻译的内容。"})
	case text == "":
		p.open(Request{Notice: "没有取到选中的文字，可以直接输入要翻译的内容。"})
	default:
		p.open(Request{Text: text})
	}
}

// translateScreen freezes the screen and lets the user drag out an area
// whose text is then translated.
func (m *Module) translateScreen() {
	p, c := m.currentPopup(), m.currentCapture()
	if p == nil || c == nil {
		return
	}
	if !platform.ScreenCaptureAllowed() {
		platform.RequestScreenCapture()
		p.open(screenPermissionRequest)
		return
	}
	err := c.start()
	switch {
	case errors.Is(err, platform.ErrNoScreenCapture):
		p.open(screenPermissionRequest)
	case err != nil:
		m.log.Warn("capture screen", "error", err)
		p.open(Request{Notice: err.Error() + "，可以直接输入要翻译的内容。"})
	}
}

var screenPermissionRequest = Request{
	Notice:     "截图翻译需要屏幕录制权限，用来读取屏幕上的文字。授权后需要重新打开 arubox。",
	Permission: permissionScreen,
}

// readScreen translates the text in the area the user dragged out of
// capture seq, given relative to the capture window.
func (m *Module) readScreen(seq int, area application.Rect) {
	p, c := m.currentPopup(), m.currentCapture()
	if p == nil || c == nil {
		return
	}
	img, ok := c.finish(seq, area)
	if !ok {
		return
	}
	lines, err := platform.ImageText(img)
	text := platform.Paragraphs(lines)
	switch {
	case err != nil:
		m.log.Warn("read screen", "error", err)
		p.open(Request{Notice: err.Error() + "，可以直接输入要翻译的内容。"})
	case text == "":
		p.open(Request{Notice: "没有在选中的区域里识别到文字。"})
	default:
		p.open(Request{Text: text})
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
var ErrDisabled = errors.New("翻译模块未启用")

// TranslateText translates text as the popup would, for other modules, and
// names the engine that did it. It returns ErrDisabled while the module is
// off.
func (m *Module) TranslateText(ctx context.Context, text string) (Translation, string, error) {
	if m.currentPopup() == nil {
		return Translation{}, "", ErrDisabled
	}
	t, err := m.translate(ctx, strings.TrimSpace(text))
	engine := "Google 翻译"
	if m.currentSettings().Engine == engineDeepL {
		engine = "DeepL"
	}
	return t, engine, err
}

// Available reports whether the module is on, for other modules.
func (m *Module) Available() bool {
	return m.currentPopup() != nil
}

// OpenText shows text, translated, in the popup.
func (m *Module) OpenText(text string) {
	if p := m.currentPopup(); p != nil {
		p.open(Request{Text: text})
	}
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
	if s.Engine == engineDeepL {
		e = &deepl{client: m.client, key: s.DeepLKey}
	}
	t, err := e.translate(ctx, text, s.Source, s.Target)
	if err != nil || s.Source != autoDetect || !sameLanguage(t.Source, s.Target) || sameLanguage(s.Second, s.Target) {
		return t, err
	}
	return e.translate(ctx, text, s.Source, s.Second)
}
