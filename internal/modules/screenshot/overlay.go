package screenshot

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shirumesu/arubox/internal/platform"
)

// Start is what the editor page needs to show a capture.
type Start struct {
	// Seq numbers captures; the page passes it back so a late call about an
	// older capture is ignored.
	Seq int `json:"seq"`
	// Width and Height are the screen's size in points. The picture has
	// the screen's pixels, so it is larger on high-density screens.
	Width   int              `json:"width"`
	Height  int              `json:"height"`
	Picture platform.Picture `json:"picture"`
	// Windows are the bounds of the windows on the screen, front to back,
	// relative to the screen, for picking one with a click.
	Windows []application.Rect `json:"windows"`
	// Translate offers translating the selection's text, when the
	// translation module is on.
	Translate bool `json:"translate"`
}

const (
	startEvent = "screenshot:start"
	endEvent   = "screenshot:end"
)

func init() {
	application.RegisterEvent[Start](startEvent)
	application.RegisterEvent[application.Void](endEvent)
}

// overlay is the window laid over the screen with the frozen picture, where
// the user selects and annotates. It is created when the module is enabled
// and hidden rather than closed, so it shows without delay; after it has
// stayed hidden for the time the user chose, it is closed to release its
// memory, and the next capture loads it again.
type overlay struct {
	app  *application.App
	keep func() time.Duration
	// ended is called whenever the overlay hides, so the capture's picture
	// is dropped.
	ended func()

	mu      sync.Mutex
	win     *application.WebviewWindow
	ready   bool   // the page has loaded
	pending *Start // a capture waiting for the page to load
	seq     int    // the capture being shown
	shows   uint64
}

// newOverlay loads the overlay hidden, to be released if unused for
// unused.
func newOverlay(app *application.App, keep func() time.Duration, unused time.Duration, ended func()) *overlay {
	o := &overlay{app: app, keep: keep, ended: ended}
	o.mu.Lock()
	o.create()
	o.scheduleRelease(unused)
	o.mu.Unlock()
	return o
}

// create loads the window hidden. o.mu must be held.
func (o *overlay) create() {
	win := o.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "screenshot",
		Title:            "截图",
		URL:              "/screenshot.html",
		Hidden:           true,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundColour: application.NewRGB(0, 0, 0),
		Mac: application.MacWindow{
			DisableShadow: true,
			CornerType:    application.MacWindowCornerTypeSquare,
			// It takes the keyboard without activating arubox, and covers
			// the menu bar and the Dock.
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
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.win == win {
			o.hide()
		}
	})
	o.win = win
	o.ready = false
}

// visible reports whether a capture is being shown.
func (o *overlay) visible() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.win != nil && o.win.IsVisible()
}

// open sends a capture of screen to the page, which shows the window once
// it has drawn the picture.
func (o *overlay) open(s Start, screen application.Rect) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.shows++
	o.seq = s.Seq
	if o.win == nil {
		o.create()
	}
	platform.SetWindowFrame(o.win, screen)
	if !o.ready {
		o.pending = &s
		return
	}
	o.win.EmitEvent(startEvent, s)
}

// attach is called by the page once it has loaded. It returns the capture
// that arrived while the page was loading, if any.
func (o *overlay) attach() *Start {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ready = true
	s := o.pending
	o.pending = nil
	return s
}

// present shows the window once the page has drawn capture seq.
func (o *overlay) present(seq int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.win == nil || seq != o.seq {
		return
	}
	o.win.Show()
	o.win.Focus()
}

// close hides the window if it shows capture seq.
func (o *overlay) close(seq int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if seq == o.seq {
		o.hide()
	}
}

// hide hides the window, tells the page to let go of the picture and
// schedules releasing the window. o.mu must be held.
func (o *overlay) hide() {
	o.pending = nil
	if o.win == nil || !o.win.IsVisible() {
		return
	}
	o.win.Hide()
	o.win.EmitEvent(endEvent)
	o.ended()
	o.scheduleRelease(o.keep())
}

// scheduleRelease closes the hidden window once it has stayed unused for
// keep, or never when keep is 0. o.mu must be held.
func (o *overlay) scheduleRelease(keep time.Duration) {
	if keep <= 0 {
		return
	}
	shows := o.shows
	time.AfterFunc(keep, func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.shows == shows && o.win != nil && !o.win.IsVisible() {
			o.destroy()
		}
	})
}

// destroy closes the window. o.mu must be held.
func (o *overlay) destroy() {
	if o.win == nil {
		return
	}
	o.win.Close()
	o.win = nil
	o.ready = false
	o.pending = nil
}
