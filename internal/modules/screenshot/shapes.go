package screenshot

import (
	"math"

	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
)

// The editor works in DIPs relative to the captured screen, as the window
// over it does; the capture itself is in physical pixels, scale times as
// many.

type tool int

const (
	toolNone tool = iota
	toolRect
	toolEllipse
	toolArrow
	toolPen
	toolText
	toolMosaic
)

// shape is an annotation.
type shape struct {
	kind     tool
	from, to ui.Point   // the corners dragged, for all but the pen and text
	points   []ui.Point // the pen's path
	text     string     // text's lines, which starts at from
	color    ui.Color
	width    float64   // the stroke width, or the text's size
	mosaic   *ui.Image // the mosaic's blocks, one pixel each
	mosaicAt ui.Rect   // the box mosaic is drawn in
	mosaicOf ui.Rect   // the box mosaic's blocks were averaged over
}

// textFamily is the font annotations are typed and drawn in: the one the
// native text box uses, so what is typed looks like what is kept.
const textFamily = "Microsoft YaHei UI"

// mosaicBlock is the width of a mosaic block in DIPs.
const mosaicBlock = 8.0

// boxFrom is the box spanned by two corners, whichever way they were
// dragged.
func boxFrom(a, b ui.Point) ui.Rect {
	return ui.Rect{X: math.Min(a.X, b.X), Y: math.Min(a.Y, b.Y), W: math.Abs(b.X - a.X), H: math.Abs(b.Y - a.Y)}
}

// contains reports whether p is in b, its edges included, so a click on
// the selection's border still counts as inside.
func contains(b ui.Rect, p ui.Point) bool {
	return p.X >= b.X && p.X <= b.X+b.W && p.Y >= b.Y && p.Y <= b.Y+b.H
}

// clampBox keeps a box inside a screen of the given size.
func clampBox(b ui.Rect, width, height float64) ui.Rect {
	w, h := math.Min(b.W, width), math.Min(b.H, height)
	return ui.Rect{X: math.Min(math.Max(b.X, 0), width-w), Y: math.Min(math.Max(b.Y, 0), height-h), W: w, H: h}
}

// clampPoint keeps a point inside a screen of the given size; the mouse
// can leave the window while a button is held.
func clampPoint(p ui.Point, width, height float64) ui.Point {
	return ui.Point{X: math.Min(math.Max(p.X, 0), width), Y: math.Min(math.Max(p.Y, 0), height)}
}

// handle is one of the selection's resize handles: a corner or the middle
// of an edge.
type handle int

const (
	handleNW handle = iota
	handleN
	handleNE
	handleE
	handleSE
	handleS
	handleSW
	handleW
	handleCount
)

func (h handle) west() bool  { return h == handleNW || h == handleW || h == handleSW }
func (h handle) east() bool  { return h == handleNE || h == handleE || h == handleSE }
func (h handle) north() bool { return h == handleNW || h == handleN || h == handleNE }
func (h handle) south() bool { return h == handleSW || h == handleS || h == handleSE }

func handlePoint(b ui.Rect, h handle) ui.Point {
	x, y := b.X+b.W/2, b.Y+b.H/2
	if h.west() {
		x = b.X
	} else if h.east() {
		x = b.X + b.W
	}
	if h.north() {
		y = b.Y
	} else if h.south() {
		y = b.Y + b.H
	}
	return ui.Point{X: x, Y: y}
}

// handleReach is how near a handle the mouse must be to grab it.
const handleReach = 6.0

// handleAt returns the handle within reach of p, if any.
func handleAt(b ui.Rect, p ui.Point) (handle, bool) {
	for h := range handleCount {
		q := handlePoint(b, h)
		if math.Abs(q.X-p.X) <= handleReach && math.Abs(q.Y-p.Y) <= handleReach {
			return h, true
		}
	}
	return 0, false
}

// resize moves the edges handle h controls by dx, dy from the box at the
// start of the drag. Dragging an edge past the opposite one flips the box.
func resize(b ui.Rect, h handle, dx, dy float64) ui.Rect {
	x0, y0, x1, y1 := b.X, b.Y, b.X+b.W, b.Y+b.H
	if h.west() {
		x0 += dx
	}
	if h.east() {
		x1 += dx
	}
	if h.north() {
		y0 += dy
	}
	if h.south() {
		y1 += dy
	}
	return boxFrom(ui.Point{X: x0, Y: y0}, ui.Point{X: x1, Y: y1})
}

func handleCursor(h handle) ui.Cursor {
	switch h {
	case handleNW, handleSE:
		return ui.CursorNWSE
	case handleNE, handleSW:
		return ui.CursorNESW
	case handleN, handleS:
		return ui.CursorNS
	}
	return ui.CursorWE
}

// windowsOn returns the windows that overlap the display, in DIPs relative
// to it and clipped to it, front to back.
func windowsOn(display ui.Bounds, scale float64, windows []platform.Rect) []ui.Rect {
	out := []ui.Rect{}
	for _, w := range windows {
		x0, y0 := max(w.X, display.X), max(w.Y, display.Y)
		x1, y1 := min(w.X+w.Width, display.X+display.W), min(w.Y+w.Height, display.Y+display.H)
		if x1 > x0 && y1 > y0 {
			out = append(out, ui.Rect{
				X: float64(x0-display.X) / scale, Y: float64(y0-display.Y) / scale,
				W: float64(x1-x0) / scale, H: float64(y1-y0) / scale,
			})
		}
	}
	return out
}

// pixelRect converts a box in DIPs to the capture's pixels, clipped to a
// capture of the given size.
func pixelRect(b ui.Rect, scale float64, width, height int) platform.Rect {
	x0 := min(max(int(math.Round(b.X*scale)), 0), width)
	y0 := min(max(int(math.Round(b.Y*scale)), 0), height)
	x1 := min(max(int(math.Round((b.X+b.W)*scale)), 0), width)
	y1 := min(max(int(math.Round((b.Y+b.H)*scale)), 0), height)
	return platform.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// drawShape draws an annotation on a canvas whose units are the screen's
// DIPs: the overlay on screen and, scaled, the finished picture.
func drawShape(c *ui.Canvas, s *shape) {
	switch s.kind {
	case toolText:
		c.Text(ui.Rect{X: s.from.X, Y: s.from.Y, W: 1e4, H: 1e4}, s.text, ui.Text{Family: textFamily, Size: s.width, Color: s.color})
	case toolPen:
		if len(s.points) == 1 {
			p := s.points[0]
			c.Line(p, p, s.width, s.color)
		} else {
			c.Polyline(s.points, s.width, s.color)
		}
	case toolRect:
		b := boxFrom(s.from, s.to)
		// A closed path of lines with round ends meeting at the first
		// corner looks like round joins all round, as a stroked rectangle
		// with round joins does.
		c.Polyline([]ui.Point{{X: b.X, Y: b.Y}, {X: b.Right(), Y: b.Y}, {X: b.Right(), Y: b.Bottom()}, {X: b.X, Y: b.Bottom()}, {X: b.X, Y: b.Y}}, s.width, s.color)
	case toolEllipse:
		b := boxFrom(s.from, s.to)
		if b.W > 0 && b.H > 0 {
			// The stroke is centred on the ellipse, not inside it.
			c.StrokeEllipse(b.Inset(-s.width/2), s.width, s.color)
		}
	case toolArrow:
		drawArrow(c, s.from, s.to, s.width, s.color)
	case toolMosaic:
		if s.mosaic != nil {
			c.Image(s.mosaic, s.mosaicAt, ui.Rect{}, 1, false)
		}
	}
}

// arrowHead returns the three corners of an arrow's head and where its
// shaft ends: inside the head, so the shaft's round end does not poke out
// of the tip.
func arrowHead(from, to ui.Point, width float64) (head []ui.Point, shaftEnd ui.Point) {
	angle := math.Atan2(to.Y-from.Y, to.X-from.X)
	size := math.Max(10, width*4)
	shaftEnd = ui.Point{X: to.X - math.Cos(angle)*size*0.6, Y: to.Y - math.Sin(angle)*size*0.6}
	head = []ui.Point{
		to,
		{X: to.X - size*math.Cos(angle-math.Pi/7), Y: to.Y - size*math.Sin(angle-math.Pi/7)},
		{X: to.X - size*math.Cos(angle+math.Pi/7), Y: to.Y - size*math.Sin(angle+math.Pi/7)},
	}
	return head, shaftEnd
}

func drawArrow(c *ui.Canvas, from, to ui.Point, width float64, col ui.Color) {
	head, end := arrowHead(from, to, width)
	c.Line(from, end, width, col)
	c.FillPolygon(head, col)
}

// mosaicGrid returns how many blocks a mosaic over b has across and down.
func mosaicGrid(b ui.Rect) (cols, rows int) {
	return max(1, int(math.Round(b.W/mosaicBlock))), max(1, int(math.Round(b.H/mosaicBlock)))
}

// mosaicPixels averages the part px of a BGRA picture into cols×rows
// blocks, one opaque pixel each. Blocks outside the picture take the
// nearest pixel in it.
func mosaicPixels(img platform.Image, px platform.Rect, cols, rows int) []byte {
	out := make([]byte, cols*rows*4)
	if img.Width == 0 || img.Height == 0 {
		return out
	}
	for j := range rows {
		y0 := px.Y + j*px.Height/rows
		y1 := max(px.Y+(j+1)*px.Height/rows, y0+1)
		y0, y1 = min(max(y0, 0), img.Height-1), min(max(y1, 1), img.Height)
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for i := range cols {
			x0 := px.X + i*px.Width/cols
			x1 := max(px.X+(i+1)*px.Width/cols, x0+1)
			x0, x1 = min(max(x0, 0), img.Width-1), min(max(x1, 1), img.Width)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var b, g, r, n int
			for y := y0; y < y1; y++ {
				row := img.Pixels[(y*img.Width+x0)*4 : (y*img.Width+x1)*4]
				for k := 0; k < len(row); k += 4 {
					b += int(row[k])
					g += int(row[k+1])
					r += int(row[k+2])
				}
				n += x1 - x0
			}
			o := (j*cols + i) * 4
			out[o], out[o+1], out[o+2], out[o+3] = byte(b/n), byte(g/n), byte(r/n), 255
		}
	}
	return out
}

// updateMosaic remakes a mosaic's blocks for its current box. While it is
// dragged out (exact false), a box that keeps its grid stretches the
// blocks already made rather than averaging every pixel under it again
// on each mouse move; the exact blocks are made when it is let go.
func updateMosaic(s *shape, img platform.Image, scale float64, exact bool) {
	b := boxFrom(s.from, s.to)
	if s.mosaic != nil && b == s.mosaicOf {
		s.mosaicAt = b
		return
	}
	if !exact && s.mosaic != nil && b.W >= 1 && b.H >= 1 {
		if cols, rows := mosaicGrid(b); cols == s.mosaic.Width && rows == s.mosaic.Height {
			s.mosaicAt = b
			return
		}
	}
	s.mosaicAt, s.mosaicOf = b, b
	if s.mosaic != nil {
		s.mosaic.Release()
		s.mosaic = nil
	}
	if b.W < 1 || b.H < 1 {
		return
	}
	cols, rows := mosaicGrid(b)
	s.mosaic = ui.NewImage(cols, rows, mosaicPixels(img, pixelRect(b, scale, img.Width, img.Height), cols, rows))
}

// composite lays premultiplied BGRA pixels over opaque ones, in place.
func composite(dst, over []byte) {
	for i := 0; i+3 < len(dst) && i+3 < len(over); i += 4 {
		a := int(over[i+3])
		if a == 0 {
			continue
		}
		k := 255 - a
		dst[i] = byte(min(int(over[i])+(int(dst[i])*k+127)/255, 255))
		dst[i+1] = byte(min(int(over[i+1])+(int(dst[i+1])*k+127)/255, 255))
		dst[i+2] = byte(min(int(over[i+2])+(int(dst[i+2])*k+127)/255, 255))
		dst[i+3] = 255
	}
}

// hexColor makes a colour from 0xRRGGBB.
func hexColor(v uint32) ui.Color {
	return ui.RGBA(uint8(v>>16), uint8(v>>8), uint8(v), 1)
}
