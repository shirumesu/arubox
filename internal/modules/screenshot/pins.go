package screenshot

import (
	"math"
	"runtime/debug"

	"github.com/shirumesu/arubox/internal/ui"
)

// Zoom limits for a pinned picture, relative to its size when pinned.
const (
	minZoom = 0.2
	maxZoom = 4.0
	// zoomStep is how much one wheel step zooms.
	zoomStep = 1.1
	// noteMillis is how long "已复制", or why the copy failed, shows.
	noteMillis = 1200
)

// pins holds the pinned pictures. Each lives until its window is closed.
// On the UI thread only.
type pins struct {
	all map[*pin]bool
}

// pin is a picture pinned on the screen, in a window of its own: drag to
// move it, scroll to zoom, double-click or Esc to close, Ctrl+C to copy.
type pin struct {
	m    *Module
	win  *ui.Window
	pic  picture
	img  *ui.Image
	size ui.Bounds // in physical pixels at zoom 1
	zoom float64

	dragX, dragY int       // the mouse, on screen, when the drag began
	dragStart    ui.Bounds // the window then
	note         string    // shown for a moment after a copy
	notes        int       // counts notes, so only the last one clears it
}

// add pins a picture at b, in physical screen pixels. Call on the UI
// thread.
func (p *pins) add(m *Module, pic picture, b ui.Bounds) {
	pn := &pin{m: m, pic: pic, img: ui.NewImage(pic.Width, pic.Height, pic.Pixels), size: b, zoom: 1}
	win, err := ui.NewWindow(ui.WindowOptions{Kind: ui.Overlay, Title: "贴图", Bounds: b, Topmost: true}, pn)
	if err != nil {
		m.log.Warn("pin screenshot", "error", err)
		return
	}
	pn.win = win
	win.OnClose = func() bool { return true }
	win.OnDestroy = func() {
		pn.img.Release()
		pn.img, pn.pic = nil, picture{}
		delete(p.all, pn)
		// The picture can be screen sized; give it back to the system now
		// rather than whenever the runtime gets to it.
		go debug.FreeOSMemory()
	}
	if p.all == nil {
		p.all = map[*pin]bool{}
	}
	p.all[pn] = true
	// It shows without taking the focus; a click on it gives it the keys.
	win.Show(false)
}

// closeAll closes every pin. Not to be called inside a frame.
func (p *pins) closeAll() {
	for pn := range p.all {
		pn.win.Close()
	}
}

// close closes the pin once the current frame is done.
func (pn *pin) close() { ui.Post(pn.win.Close) }

// Frame draws the pin and handles its input.
func (pn *pin) Frame(f *ui.Frame) {
	b := f.Bounds()
	f.Image(pn.img, b, ui.Rect{}, 1, true)
	if f.KeyPressed(ui.KeyEscape, 0) {
		pn.close()
		return
	}
	if f.KeyPressed('C', ui.ModifierCtrl) {
		pn.copy(false)
	}
	ev := f.Event()
	// The window is moved by hand rather than as a caption, so a double
	// click still reaches it.
	res := f.Interact("picture", b)
	switch {
	case res.DoubleClicked:
		// Closing by double click keeps the picture on the clipboard, so
		// it is not lost with the pin.
		pn.copy(true)
		return
	case res.Pressed:
		pn.dragX, pn.dragY = ui.CursorPos()
		pn.dragStart = pn.win.Bounds()
	case res.Held && ev.Kind == ui.EvMouseMove:
		x, y := ui.CursorPos()
		to := pn.dragStart
		to.X += x - pn.dragX
		to.Y += y - pn.dragY
		// Moving or sizing a window inside its own frame would resize its
		// drawing target mid-draw.
		ui.Post(func() { pn.win.SetBounds(to) })
	}
	if ev.Kind == ui.EvWheel && ev.Wheel != 0 {
		zoom := pn.zoom * math.Pow(zoomStep, ev.Wheel)
		if ev.Wheel > 0 {
			// The window's picture is as big as the window, so it grows no
			// bigger than its display: a 4K pin zoomed 4 times took half a
			// gigabyte. One bigger already, moved from a larger display,
			// keeps its size rather than shrinking.
			d := pn.win.Monitor().Bounds
			fit := min(float64(d.W)/float64(pn.size.W), float64(d.H)/float64(pn.size.H))
			zoom = min(zoom, max(fit, pn.zoom))
		}
		pn.zoom = min(max(zoom, minZoom), maxZoom)
		to := pn.win.Bounds()
		to.W = max(int(float64(pn.size.W)*pn.zoom), 1)
		to.H = max(int(float64(pn.size.H)*pn.zoom), 1)
		ui.Post(func() { pn.win.SetBounds(to) })
	}
	white := ui.RGBA(255, 255, 255, 1)
	if f.MouseInside() {
		r := ui.Rect{X: b.W - 6 - 24, Y: 6, W: 24, H: 24}
		if f.Interact("close", r).Clicked {
			pn.close()
		}
		f.FillEllipse(r, ui.RGBA(0, 0, 0, 0.6))
		f.Icon(r, ui.IconClose, 10, white)
	}
	if pn.note != "" {
		t := ui.Text{Size: 11, Color: white, Align: ui.AlignCenter, Middle: true}
		tw, th := ui.MeasureText(pn.note, t, 0)
		r := ui.Rect{X: (b.W - tw - 16) / 2, Y: b.H - 6 - th - 4, W: tw + 16, H: th + 4}
		f.FillRound(r, 4, ui.RGBA(0, 0, 0, 0.7))
		f.Text(r, pn.note, t)
	}
}

// copy puts the picture on the clipboard. With thenClose the pin closes
// once the picture is there; otherwise it says so for a moment. A failed
// copy keeps the pin, so the picture is not lost, and says it failed.
func (pn *pin) copy(thenClose bool) {
	pic := pn.pic
	go func() {
		err := pic.copy()
		if err != nil {
			pn.m.log.Warn("copy pinned screenshot", "error", err)
		}
		ui.Post(func() {
			switch {
			case pn.win.Closed():
			case err != nil:
				pn.show("复制失败")
			case thenClose:
				pn.win.Close()
			default:
				pn.show("已复制")
			}
		})
	}()
}

// show shows a note at the bottom of the pin for a moment.
func (pn *pin) show(note string) {
	pn.note = note
	pn.notes++
	n := pn.notes
	pn.win.Invalidate()
	pn.win.After(noteMillis, func() {
		if pn.notes == n {
			pn.note = ""
			pn.win.Invalidate()
		}
	})
}
