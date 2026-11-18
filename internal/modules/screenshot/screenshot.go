// Package screenshot is the screenshot module: select an area or a window,
// annotate it, then copy, save or pin it, or read or translate its text.
package screenshot

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
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

// TextReader reads the text in a picture with the OCR engine the user
// chose. The core provides it.
type TextReader interface {
	ImageText(img platform.Image) ([]platform.TextLine, error)
}

// stored is what the module keeps in the settings file: the folder
// pictures were last saved to.
type stored struct {
	SaveDir string `json:"saveDir,omitempty"`
}

// jpegQuality is used when a picture is saved as JPEG.
const jpegQuality = 92

// Module is the screenshot module.
type Module struct {
	store      Store
	reader     TextReader
	log        *slog.Logger
	translator Translator

	mu      sync.Mutex
	stored  stored
	enabled bool
	// busy is set from the moment a capture is taken until its editor
	// closes, so the hotkey pressed again meanwhile does nothing.
	busy bool

	// On the UI thread only.
	overlay *overlay
	pins    pins
	// The colour and width last picked, kept from one capture to the next.
	color int
	width float64
}

// New creates the module. It does nothing until enabled. translator may be
// nil, which leaves translation out.
func New(store Store, log *slog.Logger, reader TextReader, translator Translator) *Module {
	var s stored
	if err := store.ModuleSettings(id, &s); err != nil {
		log.Warn("screenshot settings unreadable, using defaults", "error", err)
		s = stored{}
	}
	return &Module{store: store, reader: reader, log: log, translator: translator, stored: s, width: widths[1]}
}

// Info describes the module.
func (m *Module) Info() module.Info {
	return module.Info{
		ID:          id,
		Name:        "截图",
		Description: "框选区域或窗口截图，标注后复制、保存或贴在屏幕上，也可以识别或翻译其中的文字。",
		Icon:        ui.IconCamera,
	}
}

// Hotkeys declares the hotkey.
func (m *Module) Hotkeys() []hotkey.Def {
	return []hotkey.Def{{ID: "capture", Label: "截图", Default: "Alt+Shift+w", Action: m.capture}}
}

// Enable readies the screen capture device in the background, so the
// first capture is as quick as the next. The editor's window is made for
// each capture and closed after it, so nothing else is held.
func (m *Module) Enable(module.Context) error {
	m.mu.Lock()
	m.enabled = true
	m.mu.Unlock()
	return nil
}

// Disable closes the editor and every pin.
func (m *Module) Disable() error {
	m.mu.Lock()
	m.enabled = false
	m.mu.Unlock()
	if m.overlay != nil {
		m.overlay.end()
	}
	m.pins.closeAll()
	return nil
}

// Settings returns nil: the dashboard's own rows enable the module and
// bind its hotkey, and there is nothing else to set.
func (m *Module) Settings() module.Page { return nil }

// capture freezes the display under the mouse and opens the editor on it.
// It runs on the hotkey's goroutine, so the capture does not hold up the
// UI thread.
func (m *Module) capture() {
	m.mu.Lock()
	if !m.enabled || m.busy {
		m.mu.Unlock()
		return
	}
	m.busy = true
	m.mu.Unlock()
	x, y := ui.CursorPos()
	display := ui.MonitorAt(x, y)
	b := display.Bounds
	img, err := platform.CaptureScreen(platform.Rect{X: b.X, Y: b.Y, Width: b.W, Height: b.H})
	if err != nil {
		m.log.Warn("capture screen", "error", err)
		m.idle()
		return
	}
	// The window draws the pixels as premultiplied, which needs them
	// opaque; the screen's alpha is not always set.
	for i := 3; i < len(img.Pixels); i += 4 {
		img.Pixels[i] = 255
	}
	start := start{
		display:   display,
		image:     img,
		windows:   platform.OnScreenWindows(),
		translate: m.translator != nil && m.translator.Available(),
		cursor:    ui.Point{X: float64(x - b.X), Y: float64(y - b.Y)},
	}
	ui.Post(func() {
		m.mu.Lock()
		enabled := m.enabled
		m.mu.Unlock()
		if !enabled {
			m.idle()
			return
		}
		o, err := openOverlay(m, start)
		if err != nil {
			m.log.Warn("open screenshot editor", "error", err)
			m.idle()
			return
		}
		m.overlay = o
	})
}

// idle lets the hotkey take the next capture.
func (m *Module) idle() {
	m.mu.Lock()
	m.busy = false
	m.mu.Unlock()
}

// recognize reads the lines of text in a picture.
func (m *Module) recognize(img platform.Image) ([]platform.TextLine, error) {
	if img.Width == 0 || img.Height == 0 {
		return nil, errors.New("没有选区")
	}
	lines, err := m.reader.ImageText(img)
	if err == nil && len(lines) == 0 {
		err = errors.New("没有识别到文字")
	}
	return lines, err
}

// joinLines returns the text of lines one per line, as they were shown.
func joinLines(lines []platform.TextLine) string {
	texts := make([]string, 0, len(lines))
	for _, l := range lines {
		texts = append(texts, l.Text)
	}
	return strings.Join(texts, "\n")
}

// picture is a finished picture: opaque top-down BGRA pixels.
type picture platform.Image

// rgba converts the picture to the standard library's image type. Being
// opaque, its pixels need no premultiplying.
func (p picture) rgba() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, p.Width, p.Height))
	for i := 0; i+3 < len(p.Pixels) && i+3 < len(out.Pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = p.Pixels[i+2], p.Pixels[i+1], p.Pixels[i], 255
	}
	return out
}

// png encodes the picture; fast trades size for speed, for the clipboard.
func (p picture) png(fast bool) ([]byte, error) {
	enc := png.Encoder{}
	if fast {
		enc.CompressionLevel = png.BestSpeed
	}
	var b bytes.Buffer
	if err := enc.Encode(&b, p.rgba()); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// copyPicture puts a picture on the clipboard. It runs on a goroutine of
// its own: encoding a large picture takes a moment.
func (m *Module) copyPicture(p picture) {
	data, err := p.png(true)
	if err == nil {
		err = platform.CopyImage(data)
	}
	if err != nil {
		m.log.Warn("copy screenshot", "error", err)
	}
}

// savePicture asks where to save a picture, as PNG or, by the name's
// extension, JPEG. It runs on a goroutine of its own, as the dialog
// blocks.
func (m *Module) savePicture(p picture) {
	if err := m.save(p); err != nil {
		m.log.Warn("save screenshot", "error", err)
	}
}

func (m *Module) save(p picture) error {
	m.mu.Lock()
	dir := m.stored.SaveDir
	m.mu.Unlock()
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, "Desktop")
		}
	}
	path, err := ui.SaveFileDialog(ui.SaveFileOptions{
		Title: "保存截图",
		Dir:   dir,
		Name:  time.Now().Format("截图 2006-01-02 15.04.05") + ".png",
		Types: []ui.FileType{
			{Name: "PNG 图片", Patterns: []string{"*.png"}},
			{Name: "JPEG 图片", Patterns: []string{"*.jpg", "*.jpeg"}},
		},
	})
	if err != nil || path == "" {
		return err
	}
	var data []byte
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".jpg" || ext == ".jpeg" {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, p.rgba(), &jpeg.Options{Quality: jpegQuality}); err != nil {
			return err
		}
		data = b.Bytes()
	} else if data, err = p.png(false); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("保存失败：%w", err)
	}
	m.rememberDir(filepath.Dir(path))
	return nil
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
