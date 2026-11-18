package translate

import (
	"fmt"
	"math"
	"runtime/debug"

	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
)

// minDrag is the smallest side, in DIPs, of an area taken as a selection;
// anything smaller is a stray click.
const minDrag = 4.0

// capture is the window laid over the display under the mouse with a
// frozen picture of it, where the user drags out the area to translate.
// The text is read from that picture, so it is what the user saw. It holds
// a screen-sized bitmap, so it exists only while shown.
type capture struct {
	m      *Module
	win    *ui.Window
	shot   platform.Image
	image  *ui.Image
	drag   bool
	from   ui.Point
	to     ui.Point
	closed bool
}

// newCapture shows shot, a picture of the display at b in physical pixels,
// over that display. Call on the UI thread.
func newCapture(m *Module, b ui.Bounds, shot platform.Image) (*capture, error) {
	// The duplicated desktop leaves alpha undefined; the picture is drawn
	// as opaque premultiplied pixels.
	for i := 3; i < len(shot.Pixels); i += 4 {
		shot.Pixels[i] = 255
	}
	c := &capture{m: m, shot: shot, image: ui.NewImage(shot.Width, shot.Height, shot.Pixels)}
	w, err := ui.NewWindow(ui.WindowOptions{Kind: ui.Overlay, Title: "截图翻译", Topmost: true, Bounds: b}, c)
	if err != nil {
		return nil, err
	}
	w.OnDeactivate = c.close
	w.OnClose = func() bool {
		c.close()
		return false
	}
	c.win = w
	w.Show(true)
	return c, nil
}

// close takes the window down and lets go of the picture.
func (c *capture) close() {
	if c.closed {
		return
	}
	c.closed = true
	if c.m.capture == c {
		c.m.capture = nil
	}
	w, img := c.win, c.image
	c.shot, c.image = platform.Image{}, nil
	if w == nil {
		return
	}
	w.Hide()
	// The window may be in the middle of its own frame; close it, and free
	// the bitmap drawn from the picture, once that is over.
	ui.Post(func() {
		w.Close()
		img.Release()
		// The picture is tens of megabytes on a large display; give it back
		// to the system now rather than whenever the runtime gets to it.
		go debug.FreeOSMemory()
	})
}

// selection is the dragged-out area in DIPs.
func (c *capture) selection() ui.Rect {
	x0, y0 := math.Min(c.from.X, c.to.X), math.Min(c.from.Y, c.to.Y)
	return ui.Rect{X: x0, Y: y0, W: math.Abs(c.to.X - c.from.X), H: math.Abs(c.to.Y - c.from.Y)}
}

// finish reads the text in area, given in DIPs of the window.
func (c *capture) finish(area ui.Rect) {
	s := c.win.Scale()
	px := func(v float64) int { return int(math.Round(v * s)) }
	img := c.shot.Crop(px(area.X), px(area.Y), px(area.W), px(area.H))
	c.close()
	go c.m.readScreen(img)
}

// Frame draws the frozen screen and the selection, and handles the drag.
func (c *capture) Frame(f *ui.Frame) {
	if c.closed {
		return
	}
	full := f.Bounds()
	f.Image(c.image, full, ui.Rect{}, 1, false)
	f.SetCursor(full, ui.CursorCross)

	if f.KeyPressed(ui.KeyEscape, 0) {
		c.close()
		return
	}
	res := f.Interact("screen", full)
	ev := f.Event()
	switch {
	case res.RightClicked:
		c.close()
		return
	case res.Pressed:
		c.drag, c.from, c.to = true, ev.Pos, ev.Pos
	case c.drag && ev.Kind == ui.EvMouseMove:
		c.to = ev.Pos
	case c.drag && ev.Kind == ui.EvMouseUp && ev.Button == 0:
		c.to = ev.Pos
		c.drag = false
		if a := c.selection(); a.W >= minDrag && a.H >= minDrag {
			c.finish(a)
			return
		}
	}

	shade := ui.Color{A: 0.35}
	if !c.drag {
		f.Fill(full, shade)
		hint := "拖动框选要翻译的文字，Esc 或右键取消"
		tw, _ := ui.MeasureText(hint, ui.Text{Size: 13}, 0)
		pill := ui.Rect{X: (full.W - tw - 32) / 2, Y: 64, W: tw + 32, H: 30}
		f.FillRound(pill, pill.H/2, ui.Color{A: 0.7})
		f.Text(pill, hint, ui.Text{Size: 13, Color: ui.Color{R: 1, G: 1, B: 1, A: 1}, Align: ui.AlignCenter, Middle: true})
		return
	}
	a := c.selection()
	// Shade everything around the selection.
	f.Fill(ui.Rect{W: full.W, H: a.Y}, shade)
	f.Fill(ui.Rect{Y: a.Bottom(), W: full.W, H: full.H - a.Bottom()}, shade)
	f.Fill(ui.Rect{Y: a.Y, W: a.X, H: a.H}, shade)
	f.Fill(ui.Rect{X: a.Right(), Y: a.Y, W: full.W - a.Right(), H: a.H}, shade)
	f.Stroke(a, 1, ui.Color{R: 1, G: 1, B: 1, A: 1}, false)

	size := fmt.Sprintf("%d × %d", int(math.Round(a.W)), int(math.Round(a.H)))
	tw, _ := ui.MeasureText(size, ui.Text{Size: 11}, 0)
	tag := ui.Rect{X: a.X, Y: math.Max(a.Y-22, 2), W: math.Ceil(tw) + 12, H: 18}
	f.FillRound(tag, 4, ui.Color{A: 0.7})
	f.Text(tag, size, ui.Text{Size: 11, Color: ui.Color{R: 1, G: 1, B: 1, A: 1}, Align: ui.AlignCenter, Middle: true})
}
