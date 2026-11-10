package screenshot

import (
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shirumesu/arubox/internal/platform"
)

// Zoom limits for a pinned picture, relative to its size on screen.
const (
	minZoom = 0.2
	maxZoom = 4.0
)

// pin is a picture pinned on the screen, in a window of its own.
type pin struct {
	win  *application.WebviewWindow
	png  []byte
	size application.Rect // the picture's size in points at zoom 1
	zoom float64
}

// pins holds the pinned pictures. Each lives until its window is closed.
type pins struct {
	app *application.App

	mu   sync.Mutex
	next int
	all  map[int]*pin
}

// add pins a PNG at r, in screen coordinates.
func (p *pins) add(png []byte, r application.Rect) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	id := p.next
	win := p.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             fmt.Sprintf("pin-%d", id),
		Title:            "贴图",
		URL:              fmt.Sprintf("/pin.html?id=%d", id),
		X:                r.X,
		Y:                r.Y,
		Width:            r.Width,
		Height:           r.Height,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundColour: application.NewRGB(0, 0, 0),
		Mac: application.MacWindow{
			CornerType: application.MacWindowCornerTypeSquare,
			// Clicking a pin does not take arubox to the front.
			WindowClass:      application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{NonActivating: true, FloatingPanel: true},
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary |
				application.MacWindowCollectionBehaviorIgnoresCycle,
		},
		Windows: application.WindowsWindow{HiddenOnTaskbar: true},
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.all, id)
	})
	if p.all == nil {
		p.all = map[int]*pin{}
	}
	p.all[id] = &pin{win: win, png: png, size: r, zoom: 1}
	platform.SetWindowFrame(win, r)
}

// picture returns a pin's PNG.
func (p *pins) picture(id int) ([]byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pn, ok := p.all[id]; ok {
		return pn.png, true
	}
	return nil, false
}

// zoom scales a pin by factor around its top-left corner.
func (p *pins) zoom(id int, factor float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pn, ok := p.all[id]
	if !ok {
		return
	}
	pn.zoom = min(max(pn.zoom*factor, minZoom), maxZoom)
	b := pn.win.Bounds()
	b.Width = max(int(float64(pn.size.Width)*pn.zoom), 1)
	b.Height = max(int(float64(pn.size.Height)*pn.zoom), 1)
	platform.SetWindowFrame(pn.win, b)
}

func (p *pins) close(id int) {
	p.mu.Lock()
	pn, ok := p.all[id]
	p.mu.Unlock()
	if ok {
		pn.win.Close()
	}
}

// closeAll closes every pin.
func (p *pins) closeAll() {
	p.mu.Lock()
	all := make([]*pin, 0, len(p.all))
	for _, pn := range p.all {
		all = append(all, pn)
	}
	p.mu.Unlock()
	for _, pn := range all {
		pn.win.Close()
	}
}
