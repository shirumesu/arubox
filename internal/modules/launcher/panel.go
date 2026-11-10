package launcher

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shirumesu/arubox/internal/platform"
)

const (
	panelWidth = 640
	// panelTop places the panel's top this far down the screen's work area,
	// as a fraction of its height, like Spotlight.
	panelTop = 0.22
)

const openEvent = "launcher:open"

func init() {
	application.RegisterEvent[application.Void](openEvent)
}

// panel is the launcher window. It is created when the module is enabled
// and hidden rather than closed, so it shows without delay; after it has
// stayed hidden for the time the user chose, it is closed to release its
// memory, and the next open loads it again.
type panel struct {
	app  *application.App
	keep func() time.Duration

	mu      sync.Mutex
	win     *application.WebviewWindow
	ready   bool // the page has loaded
	pending bool // an open waits for the page to load
	area    application.Rect
	// height is the page's latest reported height. The page numbers its
	// reports, because calls can arrive out of order.
	height    int
	heightSeq int
	// opens counts opens, so a release timer that fires after the panel
	// was opened again leaves it alone.
	opens uint64
}

func newPanel(app *application.App, keep func() time.Duration) *panel {
	p := &panel{app: app, keep: keep}
	p.mu.Lock()
	p.create()
	p.mu.Unlock()
	return p
}

// create loads the window hidden. p.mu must be held.
func (p *panel) create() {
	background := application.NewRGB(255, 255, 255)
	if p.app.Env.IsDarkMode() {
		background = application.NewRGB(38, 38, 38)
	}
	win := p.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "launcher",
		Title:            "启动器",
		Width:            panelWidth,
		Height:           64,
		URL:              "/launcher.html",
		Hidden:           true,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundColour: background,
		Mac: application.MacWindow{
			// A non-activating panel takes the keyboard without bringing
			// arubox to the front, like Spotlight.
			WindowClass:      application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{NonActivating: true, FloatingPanel: true},
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary |
				application.MacWindowCollectionBehaviorIgnoresCycle,
		},
		Windows: application.WindowsWindow{HiddenOnTaskbar: true},
	})
	win.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.win == win {
			p.hide()
		}
	})
	p.win = win
	p.ready = false
	p.heightSeq = 0
}

// toggle shows the panel on the screen under the mouse, or hides it if it
// is showing.
func (p *panel) toggle() {
	area := p.app.Screen.ScreenNearestDipPoint(platform.CursorPosition()).WorkArea
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.win != nil && p.win.IsVisible() {
		p.hide()
		return
	}
	p.opens++
	p.area = area
	if p.win == nil {
		p.create()
	}
	if !p.ready {
		p.pending = true
		return
	}
	p.win.EmitEvent(openEvent)
}

// attach is called by the page once it has loaded. It reports whether an
// open waited for it.
func (p *panel) attach() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ready = true
	pending := p.pending
	p.pending = false
	return pending
}

// present shows the panel once the page has drawn, at the page's height.
func (p *panel) present(seq, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.win == nil {
		return
	}
	p.setHeight(seq, height)
	p.fit()
	p.win.Show()
	p.win.Focus()
}

// resize follows the page's height while the panel is shown, and remembers
// it for the next present while it is not.
func (p *panel) resize(seq, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setHeight(seq, height)
	if p.win != nil && p.win.IsVisible() {
		p.fit()
	}
}

// setHeight records a height report unless a newer one arrived first. p.mu
// must be held.
func (p *panel) setHeight(seq, height int) {
	if seq > p.heightSeq {
		p.heightSeq, p.height = seq, height
	}
}

// fit places the panel at its height, growing downwards from a fixed top.
// p.mu must be held.
func (p *panel) fit() {
	platform.SetWindowFrame(p.win, place(p.height, p.area))
}

func (p *panel) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hide()
}

// hide hides the panel and schedules releasing it. p.mu must be held.
func (p *panel) hide() {
	p.pending = false
	if p.win == nil || !p.win.IsVisible() {
		return
	}
	p.win.Hide()
	keep := p.keep()
	if keep <= 0 {
		return
	}
	opens := p.opens
	time.AfterFunc(keep, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.opens == opens && p.win != nil && !p.win.IsVisible() {
			p.destroy()
		}
	})
}

// destroy closes the window. p.mu must be held.
func (p *panel) destroy() {
	if p.win == nil {
		return
	}
	p.win.Close()
	p.win = nil
	p.ready = false
	p.pending = false
}

// place centres the panel horizontally in the work area with its top a
// fixed way down, keeping it inside the area however tall it grows.
func place(height int, area application.Rect) application.Rect {
	height = min(height, area.Height)
	x := area.X + (area.Width-panelWidth)/2
	y := area.Y + int(float64(area.Height)*panelTop)
	y = min(y, area.Y+area.Height-height)
	return application.Rect{X: x, Y: y, Width: panelWidth, Height: height}
}
