package translate

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shirumesu/arubox/internal/platform"
)

// Frame tells the capture page to show a frozen screen.
type Frame struct {
	// Seq numbers captures; the page passes it back so a late call about an
	// older capture is ignored.
	Seq     int              `json:"seq"`
	Picture platform.Picture `json:"picture"`
}

const (
	frameEvent    = "translate:capture"
	frameEndEvent = "translate:capture-end"
	// frameRoute is where the capture page loads the frozen screen from, a
	// strip at a time as /translate/capture?part=N. Wails strips the route
	// before serving.
	frameRoute = "/translate/"
	framePath  = "capture"
)

func init() {
	application.RegisterEvent[Frame](frameEvent)
	application.RegisterEvent[application.Void](frameEndEvent)
}

// capture is the window laid over the screen under the mouse with a frozen
// picture of it, where the user drags out the area to translate. The text
// is read from that picture, so it is what the user saw. Like the popup, it
// is loaded when the module is enabled and hidden rather than closed, since
// a window that has just loaded shows its first frames only after a second;
// it is released after it has stayed hidden for the time the user chose.
type capture struct {
	app  *application.App
	keep func() time.Duration

	mu      sync.Mutex
	win     *application.WebviewWindow
	ready   bool   // the page has loaded
	pending *Frame // a capture waiting for the page to load
	seq     int    // the capture being shown
	screen  application.Rect
	image   platform.Image // the frozen screen, until the window hides
	// starts counts captures, so a release timer that fires after the
	// window was used again leaves it alone.
	starts uint64
}

// create loads the window hidden. c.mu must be held.
func (c *capture) create() {
	win := c.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "capture",
		Title:            "截图翻译",
		URL:              "/capture.html",
		Hidden:           true,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundColour: application.NewRGB(0, 0, 0),
		Mac: application.MacWindow{
			DisableShadow: true,
			CornerType:    application.MacWindowCornerTypeSquare,
			// Like the popup, it takes the keyboard without activating
			// arubox, and it covers the menu bar and the Dock.
			WindowClass:      application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{NonActivating: true, FloatingPanel: true},
			WindowLevel:      application.MacWindowLevelScreenSaver,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary |
				application.MacWindowCollectionBehaviorIgnoresCycle,
		},
		Windows: application.WindowsWindow{HiddenOnTaskbar: true},
	})
	win.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.win == win {
			c.hide()
		}
	})
	c.win = win
	c.ready = false
}

// visible reports whether a capture is being shown.
func (c *capture) visible() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.win != nil && c.win.IsVisible()
}

// start freezes the screen under the mouse and sends the picture to the
// page, which shows the window once it has drawn it.
func (c *capture) start() error {
	if c.visible() {
		return nil
	}
	screen := c.app.Screen.ScreenNearestDipPoint(platform.CursorPosition()).Bounds
	img, err := platform.CaptureScreen(screen)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts++
	c.seq++
	c.screen, c.image = screen, img
	if c.win == nil {
		c.create()
	}
	platform.SetWindowFrame(c.win, screen)
	frame := Frame{Seq: c.seq, Picture: img.Picture()}
	if !c.ready {
		c.pending = &frame
		return nil
	}
	c.win.EmitEvent(frameEvent, frame)
	return nil
}

// attach is called by the page once it has loaded. It returns the capture
// that arrived while the page was loading, if any.
func (c *capture) attach() *Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready = true
	f := c.pending
	c.pending = nil
	return f
}

// present shows the window once the page has drawn capture seq.
func (c *capture) present(seq int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.win == nil || seq != c.seq {
		return
	}
	c.win.Show()
	c.win.Focus()
}

// picture returns the frozen screen being shown.
func (c *capture) picture() (platform.Image, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.image, c.image.Width > 0
}

// finish hides the window of capture seq and returns the part of its
// picture inside area, given in points relative to the window.
func (c *capture) finish(seq int, area application.Rect) (platform.Image, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	img, screen := c.image, c.screen
	if seq != c.seq || img.Width == 0 {
		return platform.Image{}, false
	}
	c.hide()
	// The picture has the screen's pixels, so it is larger than the window
	// on high-density screens.
	scale := float64(img.Width) / float64(screen.Width)
	px := func(v int) int { return int(float64(v)*scale + 0.5) }
	return img.Crop(px(area.X), px(area.Y), px(area.Width), px(area.Height)), true
}

func (c *capture) cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hide()
}

// hide hides the window, tells the page to let go of the picture and
// schedules releasing the window. c.mu must be held.
func (c *capture) hide() {
	c.pending = nil
	c.image = platform.Image{}
	if c.win == nil || !c.win.IsVisible() {
		return
	}
	c.win.Hide()
	c.win.EmitEvent(frameEndEvent)
	c.scheduleRelease(c.keep())
}

// scheduleRelease closes the hidden window once it has stayed unused for
// keep, or never when keep is 0. c.mu must be held.
func (c *capture) scheduleRelease(keep time.Duration) {
	if keep <= 0 {
		return
	}
	starts := c.starts
	time.AfterFunc(keep, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.starts == starts && c.win != nil && !c.win.IsVisible() {
			c.destroy()
		}
	})
}

// destroy closes the window. c.mu must be held.
func (c *capture) destroy() {
	if c.win == nil {
		return
	}
	c.win.Close()
	c.win = nil
	c.ready = false
	c.pending = nil
	c.image = platform.Image{}
}

// frames serves the frozen screen to the capture page, so it never travels
// as text through the bindings.
type frames struct {
	module *Module
}

func (h frames) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	c := h.module.currentCapture()
	if c == nil || r.URL.Path != framePath {
		http.NotFound(w, r)
		return
	}
	img, ok := c.picture()
	part, err := strconv.Atoi(r.URL.Query().Get("part"))
	if !ok || err != nil {
		http.NotFound(w, r)
		return
	}
	img.ServeStrip(w, r, part)
}
