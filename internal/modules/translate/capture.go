package translate

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shirumesu/arubox/internal/platform"
)

// capture is the see-through window laid over the screen under the mouse,
// where the user drags out the area to translate. It is created on first
// use and hidden rather than closed, like the popup, and released after it
// has stayed hidden for the time the user chose.
type capture struct {
	app  *application.App
	keep func() time.Duration

	mu      sync.Mutex
	win     *application.WebviewWindow
	ready   bool // the page has loaded
	pending bool // a capture waits for the page to load
	screen  application.Rect
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
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Mac: application.MacWindow{
			Backdrop:      application.MacBackdropTransparent,
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

// start covers the screen under the mouse.
func (c *capture) start() {
	screen := c.app.Screen.ScreenNearestDipPoint(platform.CursorPosition()).Bounds
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts++
	c.screen = screen
	if c.win == nil {
		c.create()
	}
	platform.SetWindowFrame(c.win, screen)
	if !c.ready {
		c.pending = true
		return
	}
	c.show()
}

// show shows the window. c.mu must be held.
func (c *capture) show() {
	c.win.Show()
	c.win.Focus()
}

// attach is called by the page once it has loaded, and shows the window if
// a capture waited for it.
func (c *capture) attach() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready = true
	if c.pending && c.win != nil {
		c.pending = false
		c.show()
	}
}

// finish hides the window and returns the dragged area, given relative to
// the window, in screen coordinates.
func (c *capture) finish(area application.Rect) application.Rect {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hide()
	area.X += c.screen.X
	area.Y += c.screen.Y
	return area
}

func (c *capture) cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hide()
}

// hide hides the window and schedules releasing it. c.mu must be held.
func (c *capture) hide() {
	c.pending = false
	if c.win == nil || !c.win.IsVisible() {
		return
	}
	c.win.Hide()
	keep := c.keep()
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
	c.pending = false
}
