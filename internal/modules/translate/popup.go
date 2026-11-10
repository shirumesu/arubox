package translate

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shirumesu/arubox/internal/platform"
)

const (
	popupWidth = 420
	// popupGap keeps the popup this far from the cursor and the screen edges.
	popupGap = 12
)

// Request is what the popup shows when it opens.
type Request struct {
	// Text is translated right away; empty opens the popup for typing.
	Text string `json:"text"`
	// Notice explains why Text is empty when a selection was expected.
	Notice string `json:"notice,omitempty"`
	// Permission names the missing permission whose system settings the
	// popup offers to open: "accessibility" or "screen".
	Permission string `json:"permission,omitempty"`
}

// The permissions macOS asks the user for.
const (
	permissionAccessibility = "accessibility"
	permissionScreen        = "screen"
)

const (
	openEvent   = "translate:open"
	hiddenEvent = "translate:hidden"
)

func init() {
	application.RegisterEvent[Request](openEvent)
	application.RegisterEvent[application.Void](hiddenEvent)
}

// popup is the floating translation window. It is created when the module is
// enabled and hidden rather than closed, so it opens without delay. After it
// has stayed hidden for the time the user chose, it is closed to release its
// memory, and the next open loads it again.
type popup struct {
	app  *application.App
	keep func() time.Duration

	mu      sync.Mutex
	win     *application.WebviewWindow
	ready   bool     // the page has loaded and listens for requests
	pending *Request // a request waiting for the page to load
	anchor  application.Point
	pinned  bool
	// height is the page's latest reported height. The page numbers its
	// reports, because calls can arrive out of order.
	height    int
	heightSeq int
	// opens counts opens, so a release timer that fires after the popup was
	// opened again leaves it alone.
	opens uint64
}

func newPopup(app *application.App, keep func() time.Duration) *popup {
	p := &popup{app: app, keep: keep}
	p.mu.Lock()
	p.create()
	p.mu.Unlock()
	return p
}

// create loads the window hidden. p.mu must be held.
func (p *popup) create() {
	background := application.NewRGB(255, 255, 255)
	if p.app.Env.IsDarkMode() {
		background = application.NewRGB(38, 38, 38)
	}
	win := p.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "translator",
		Title:            "翻译",
		Width:            popupWidth,
		Height:           160,
		URL:              "/translator.html",
		Hidden:           true,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundColour: background,
		Mac: application.MacWindow{
			// A non-activating panel takes the keyboard without bringing
			// arubox to the front, so the program the text came from stays
			// active and keeps its selection.
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
		if !p.pinned && p.win == win {
			p.hide()
		}
	})
	p.win = win
	p.ready = false
}

// open shows req in the popup, next to the mouse.
func (p *popup) open(req Request) {
	anchor := platform.CursorPosition()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opens++
	// A pinned popup stays where the user put it.
	if !p.pinned {
		p.anchor = anchor
	}
	if p.win == nil {
		p.create()
	}
	if !p.ready {
		p.pending = &req
		return
	}
	p.win.EmitEvent(openEvent, req)
}

// attach is called by the page once it listens for requests. It returns the
// request that arrived while the page was loading, if any.
func (p *popup) attach() *Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ready = true
	req := p.pending
	p.pending = nil
	return req
}

// present shows the popup once the page has drawn the request, so the
// previous content never flashes.
func (p *popup) present(seq, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.win == nil {
		return
	}
	p.setHeight(seq, height)
	if p.pinned && p.win.IsVisible() {
		p.fit()
	} else {
		area := p.app.Screen.ScreenNearestDipPoint(p.anchor).WorkArea
		platform.SetWindowFrame(p.win, place(p.anchor, p.height, area))
	}
	p.win.Show()
	p.win.Focus()
}

// resize follows the page's height while the popup is shown, and remembers
// it for the next present while it is not.
func (p *popup) resize(seq, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setHeight(seq, height)
	if p.win != nil && p.win.IsVisible() {
		p.fit()
	}
}

// setHeight records a height report unless a newer one arrived first. p.mu
// must be held.
func (p *popup) setHeight(seq, height int) {
	if seq > p.heightSeq {
		p.heightSeq, p.height = seq, height
	}
}

// fit resizes the popup in place to the page's height, moving it up if it
// would run off the bottom of the screen. p.mu must be held.
func (p *popup) fit() {
	b := p.win.Bounds()
	area := p.app.Screen.ScreenNearestDipPoint(application.Point{X: b.X, Y: b.Y}).WorkArea
	b.Width = popupWidth
	b.Height = min(p.height, area.Height-2*popupGap)
	b.Y = min(b.Y, area.Y+area.Height-popupGap-b.Height)
	platform.SetWindowFrame(p.win, b)
}

func (p *popup) setPinned(pinned bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pinned = pinned
}

func (p *popup) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hide()
}

// hide hides the popup and schedules releasing it. p.mu must be held.
func (p *popup) hide() {
	if p.win == nil || !p.win.IsVisible() {
		return
	}
	p.pinned = false
	p.win.Hide()
	p.win.EmitEvent(hiddenEvent)
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
func (p *popup) destroy() {
	if p.win == nil {
		return
	}
	p.win.Close()
	p.win = nil
	p.ready = false
	p.pending = nil
	p.pinned = false
	// A new page numbers its reports from the start again.
	p.heightSeq = 0
}

// place puts a popup of the given height below and to the right of the
// cursor, inside the screen's work area.
func place(cursor application.Point, height int, area application.Rect) application.Rect {
	height = min(height, area.Height-2*popupGap)
	x := cursor.X + popupGap
	y := cursor.Y + popupGap
	x = max(min(x, area.X+area.Width-popupGap-popupWidth), area.X+popupGap)
	y = max(min(y, area.Y+area.Height-popupGap-height), area.Y+popupGap)
	return application.Rect{X: x, Y: y, Width: popupWidth, Height: height}
}
