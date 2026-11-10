// Package screenshot is the screenshot module: select an area or a window,
// annotate it, then copy, save or pin it, or read or translate its text.
package screenshot

import (
	"bytes"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/platform"
)

const id = "screenshot"

// Store keeps the module's settings. config.Store satisfies it.
type Store interface {
	ModuleSettings(id string, v any) error
	SetModuleSettings(id string, v any) error
}

// Translator shows text in the translation popup. The core provides it from
// the translation module.
type Translator interface {
	// Available reports whether the translation module is on.
	Available() bool
	// Open shows text, translated, in the translation popup.
	Open(text string)
}

// Settings is what the user can change about the module.
type Settings struct {
	// KeepMinutes is how long the hidden editor is kept ready before its
	// memory is released. 0 keeps it for good.
	KeepMinutes int `json:"keepMinutes"`
}

// stored is what the module keeps in the settings file: the settings and
// the folder pictures were last saved to.
type stored struct {
	Settings
	SaveDir string `json:"saveDir,omitempty"`
}

func defaultStored() stored {
	return stored{Settings: Settings{KeepMinutes: 30}}
}

// jpegQuality is used when a picture is saved as JPEG.
const jpegQuality = 92

// Module is the screenshot module.
type Module struct {
	store      Store
	log        *slog.Logger
	translator Translator
	pins       *pins

	mu      sync.Mutex
	stored  stored
	app     *application.App
	overlay *overlay // nil while disabled
	seq     int
	screen  application.Rect // the captured screen, in points
	image   platform.Image   // the capture being edited, if any
	asked   bool             // the screen recording permission was asked for
}

// New creates the module. It does nothing until enabled. translator may be
// nil, which leaves translation out.
func New(store Store, log *slog.Logger, translator Translator) *Module {
	s := defaultStored()
	if err := store.ModuleSettings(id, &s); err != nil {
		log.Warn("screenshot settings unreadable, using defaults", "error", err)
		s = defaultStored()
	}
	return &Module{store: store, log: log, translator: translator, stored: s, pins: &pins{}}
}

// Info describes the module.
func (m *Module) Info() module.Info {
	return module.Info{
		ID:          id,
		Name:        "截图",
		Description: "框选区域或窗口截图，标注后复制、保存或贴在屏幕上，也可以识别或翻译其中的文字。",
		Platforms:   []string{"darwin", "windows"},
	}
}

// Services binds the editor, the pins and the settings page, and serves
// the pictures to them.
func (m *Module) Services() []application.Service {
	return []application.Service{
		application.NewService(&Service{module: m}),
		application.NewServiceWithOptions(&images{module: m}, application.ServiceOptions{Route: imageRoute}),
	}
}

// Hotkeys declares the hotkey.
func (m *Module) Hotkeys() []hotkey.Def {
	capture := "Alt+w"
	if runtime.GOOS == "windows" {
		capture = "Alt+Shift+w"
	}
	return []hotkey.Def{{ID: "capture", Label: "截图", Default: capture, Action: m.capture}}
}

// Enable loads the editor in the background so the first capture shows
// without delay.
func (m *Module) Enable(ctx module.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.app = ctx.App
	m.pins.app = ctx.App
	m.overlay = newOverlay(ctx.App, m.keepTime, m.keepLocked(), m.dropImage)
	go platform.PrepareScreenCapture()
	return nil
}

// Disable closes the editor and every pin.
func (m *Module) Disable() error {
	m.mu.Lock()
	o := m.overlay
	m.overlay = nil
	m.image = platform.Image{}
	m.mu.Unlock()
	if o != nil {
		o.mu.Lock()
		o.destroy()
		o.mu.Unlock()
	}
	m.pins.closeAll()
	return nil
}

func (m *Module) keepTime() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keepLocked()
}

// keepLocked is keepTime with m.mu held.
func (m *Module) keepLocked() time.Duration {
	return time.Duration(m.stored.KeepMinutes) * time.Minute
}

func (m *Module) currentOverlay() *overlay {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.overlay
}

// capture freezes the screen under the mouse and opens the editor on it.
func (m *Module) capture() {
	o := m.currentOverlay()
	if o == nil || o.visible() {
		return
	}
	if !platform.ScreenCaptureAllowed() {
		m.askPermission()
		return
	}
	screen := m.app.Screen.ScreenNearestDipPoint(platform.CursorPosition()).Bounds
	img, err := platform.CaptureScreen(screen)
	if errors.Is(err, platform.ErrNoScreenCapture) {
		m.askPermission()
		return
	}
	if err != nil {
		m.log.Warn("capture screen", "error", err)
		return
	}
	start := Start{
		Width:     screen.Width,
		Height:    screen.Height,
		Picture:   img.Picture(),
		Windows:   windowsOn(screen, platform.OnScreenWindows()),
		Translate: m.translator != nil && m.translator.Available(),
	}
	m.mu.Lock()
	m.seq++
	start.Seq = m.seq
	m.screen, m.image = screen, img
	m.mu.Unlock()
	o.open(start, screen)
}

// askPermission shows the system's prompt the first time and opens System
// Settings after that, when the prompt no longer appears.
func (m *Module) askPermission() {
	m.mu.Lock()
	asked := m.asked
	m.asked = true
	m.mu.Unlock()
	if asked {
		platform.OpenScreenCaptureSettings()
		return
	}
	platform.RequestScreenCapture()
}

// windowsOn returns the windows that overlap screen, relative to it and
// clipped to it.
func windowsOn(screen application.Rect, windows []application.Rect) []application.Rect {
	out := []application.Rect{}
	for _, w := range windows {
		x0, y0 := max(w.X, screen.X), max(w.Y, screen.Y)
		x1, y1 := min(w.X+w.Width, screen.X+screen.Width), min(w.Y+w.Height, screen.Y+screen.Height)
		if x1 > x0 && y1 > y0 {
			out = append(out, application.Rect{X: x0 - screen.X, Y: y0 - screen.Y, Width: x1 - x0, Height: y1 - y0})
		}
	}
	return out
}

// currentImage returns the capture being edited.
func (m *Module) currentImage() (platform.Image, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.image, m.image.Width > 0
}

// dropImage lets go of the capture once the editor hides.
func (m *Module) dropImage() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.image = platform.Image{}
}

// finish hides the editor of capture seq.
func (m *Module) finish(seq int) {
	if o := m.currentOverlay(); o != nil {
		o.close(seq)
	}
}

// recognize reads the lines of text in part of capture seq, given in its
// pixels. The editor stays open meanwhile, so it can show a failure.
func (m *Module) recognize(seq int, r application.Rect) ([]platform.TextLine, error) {
	m.mu.Lock()
	img, current := m.image, seq == m.seq
	m.mu.Unlock()
	if !current || img.Width == 0 {
		return nil, errors.New("截图已失效")
	}
	lines, err := platform.ImageText(img.Crop(r.X, r.Y, r.Width, r.Height))
	if err == nil && len(lines) == 0 {
		err = errors.New("没有识别到文字")
	}
	return lines, err
}

// readText returns the text in part of capture seq, line by line as shown.
func (m *Module) readText(seq int, r application.Rect) (string, error) {
	lines, err := m.recognize(seq, r)
	if err != nil {
		return "", err
	}
	texts := make([]string, 0, len(lines))
	for _, l := range lines {
		texts = append(texts, l.Text)
	}
	return strings.Join(texts, "\n"), nil
}

// copyText closes the editor of capture seq and copies text.
func (m *Module) copyText(seq int, text string) error {
	m.finish(seq)
	if !m.app.Clipboard.SetText(text) {
		return errors.New("复制失败")
	}
	return nil
}

// translate shows the text in part of capture seq in the translation popup,
// with lines broken inside a sentence joined back.
func (m *Module) translate(seq int, r application.Rect) error {
	if m.translator == nil || !m.translator.Available() {
		return errors.New("翻译模块未启用")
	}
	lines, err := m.recognize(seq, r)
	if err != nil {
		return err
	}
	m.finish(seq)
	m.translator.Open(platform.Paragraphs(lines))
	return nil
}

// copyImage puts the finished picture on the clipboard.
func (m *Module) copyImage(seq int, pngData []byte) error {
	m.finish(seq)
	return platform.CopyImage(pngData)
}

// pin pins the finished picture where it was selected, at r relative to
// the captured screen.
func (m *Module) pin(seq int, pngData []byte, r application.Rect) {
	m.mu.Lock()
	screen := m.screen
	m.mu.Unlock()
	m.finish(seq)
	r.X += screen.X
	r.Y += screen.Y
	m.pins.add(pngData, r)
}

// save asks where to save the finished picture, as PNG or, by the name's
// extension, JPEG.
func (m *Module) save(seq int, pngData []byte) error {
	m.finish(seq)
	m.mu.Lock()
	dir := m.stored.SaveDir
	m.mu.Unlock()
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, "Desktop")
		}
	}
	path, err := m.app.Dialog.SaveFile().
		SetFilename(time.Now().Format("截图 2006-01-02 15.04.05")+".png").
		SetDirectory(dir).
		CanCreateDirectories(true).
		AddFilter("PNG 图片", "*.png").
		AddFilter("JPEG 图片", "*.jpg;*.jpeg").
		PromptForSingleSelection()
	if err != nil || path == "" {
		return err
	}
	data := pngData
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".jpg" || ext == ".jpeg" {
		if data, err = toJPEG(pngData); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("保存失败：%w", err)
	}
	m.rememberDir(filepath.Dir(path))
	return nil
}

func toJPEG(pngData []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (m *Module) rememberDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.stored
	next.SaveDir = dir
	if err := m.store.SetModuleSettings(id, next); err != nil {
		m.log.Warn("save screenshot folder", "error", err)
		return
	}
	m.stored = next
}

func (m *Module) currentSettings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stored.Settings
}

func (m *Module) setSettings(s Settings) error {
	if s.KeepMinutes < 0 {
		return errors.New("保留时间不能为负数")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.stored
	next.Settings = s
	if err := m.store.SetModuleSettings(id, next); err != nil {
		return errors.New("保存设置失败：" + err.Error())
	}
	m.stored = next
	return nil
}
