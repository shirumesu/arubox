package ui

import "math"

// EventKind says what happened.
type EventKind int

// Event kinds. EvNone is a plain repaint.
const (
	EvNone EventKind = iota
	EvMouseMove
	EvMouseDown
	EvMouseUp
	EvDoubleClick
	EvMouseLeave
	EvWheel
	EvKeyDown
	EvChar
	EvTextChanged
)

// Mods is a set of held modifier keys.
type Mods uint8

// Modifier keys.
const (
	ModifierCtrl Mods = 1 << iota
	ModifierShift
	ModifierAlt
	ModifierWin
)

// Virtual key codes the views use.
const (
	KeyBack   = 0x08
	KeyTab    = 0x09
	KeyReturn = 0x0D
	KeyEscape = 0x1B
	KeySpace  = 0x20
	KeyPageUp = 0x21
	KeyPageDn = 0x22
	KeyEnd    = 0x23
	KeyHome   = 0x24
	KeyLeft   = 0x25
	KeyUp     = 0x26
	KeyRight  = 0x27
	KeyDown   = 0x28
	KeyDelete = 0x2E
)

// Event is the input a frame runs for.
type Event struct {
	Kind   EventKind
	Pos    Point // mouse position in DIPs
	Button int   // 0 left, 1 right, 2 middle
	Wheel  float64
	Key    int // virtual key code
	Char   rune
	Mods   Mods
	// ID names the text box a key or a change came from.
	ID string
}

type inputState struct {
	pos     Point
	inside  bool
	down    int // button held, -1 for none
	downPos Point
	active  string // the region the held button went down on
}

type region struct {
	id  string
	r   Rect
	seq int
}

type dragArea struct {
	r   Rect
	seq int
}

// Frame is one run of a view: the canvas to draw on and the input to
// react to.
type Frame struct {
	*Canvas
	w    *Window
	ev   Event
	size Point
	// clip is the visible part of the current scroll area, in its own
	// coordinates; interaction outside it is ignored.
	clip *Rect
}

// Window returns the window being drawn.
func (f *Frame) Window() *Window { return f.w }

// Size returns the window's client size in DIPs.
func (f *Frame) Size() (w, h float64) { return f.size.X, f.size.Y }

// Bounds returns the client area in DIPs.
func (f *Frame) Bounds() Rect { return Rect{0, 0, f.size.X, f.size.Y} }

// Event returns the input this frame runs for.
func (f *Frame) Event() Event {
	ev := f.ev
	ev.Pos = f.local(ev.Pos)
	return ev
}

// Mouse returns the mouse position in the current coordinates.
func (f *Frame) Mouse() Point { return f.local(f.w.input.pos) }

// MouseInside reports whether the mouse is over the window.
func (f *Frame) MouseInside() bool { return f.w.input.inside }

// Held reports whether the button went down on region id and is still
// held.
func (f *Frame) Held(id string) bool { return f.w.input.down >= 0 && f.w.input.active == id }

// Active reports whether the window has the focus.
func (f *Frame) Active() bool { return f.w.Active() }

// Invalidate asks for another frame after this one, to show state the
// frame changed after drawing what depended on it.
func (f *Frame) Invalidate() { f.w.again = true }

// Key returns the key pressed, when this frame runs for one. It returns
// ok false otherwise.
func (f *Frame) Key() (vk int, mods Mods, ok bool) {
	if f.ev.Kind != EvKeyDown {
		return 0, 0, false
	}
	return f.ev.Key, f.ev.Mods, true
}

// KeyPressed reports whether this frame runs for vk with exactly mods
// held, and takes the key, so a text box does not get it.
func (f *Frame) KeyPressed(vk int, mods Mods) bool {
	if f.ev.Kind == EvKeyDown && f.ev.Key == vk && f.ev.Mods == mods {
		f.w.keyTaken = true
		f.w.again = true
		return true
	}
	return false
}

// TakeKey keeps the current key from reaching a text box.
func (f *Frame) TakeKey() {
	f.w.keyTaken = true
	f.w.again = true
}

func (f *Frame) local(p Point) Point { return Point{p.X - f.ox/1, p.Y - f.oy/1} }

func (f *Frame) global(r Rect) Rect { return Rect{r.X + f.ox, r.Y + f.oy, r.W, r.H} }

// Response tells a widget what the mouse did with it this frame.
type Response struct {
	Hovered       bool
	Pressed       bool // the button went down on it this frame
	Held          bool // the button is held after going down on it
	Clicked       bool // left button released over it after pressing it
	RightClicked  bool
	DoubleClicked bool
	// Drag is how far the mouse moved since the button went down on it.
	Drag Point
}

// Interact registers r as the region id for this frame and reports what
// the mouse did with it. Later regions are in front of earlier ones.
func (f *Frame) Interact(id string, r Rect) Response {
	g := f.global(r)
	if f.clip != nil {
		g = g.Intersect(f.global(*f.clip))
	}
	f.w.seq++
	if !g.Empty() {
		f.w.regions = append(f.w.regions, region{id: id, r: g, seq: f.w.seq})
	}
	in := f.w.input
	var res Response
	hot := in.inside && f.w.hotAt(in.pos) == id && g.Contains(in.pos)
	res.Hovered = hot && (in.down < 0 || in.active == id)
	res.Held = in.down >= 0 && in.active == id
	if res.Held {
		res.Drag = Point{in.pos.X - in.downPos.X, in.pos.Y - in.downPos.Y}
	}
	switch f.ev.Kind {
	case EvMouseDown:
		res.Pressed = in.active == id && f.ev.Button == 0
		if in.active == id && f.ev.Button == 1 {
			res.RightClicked = true
		}
	case EvDoubleClick:
		res.DoubleClicked = in.active == id
		res.Pressed = res.DoubleClicked
	case EvMouseUp:
		res.Clicked = in.active == id && f.ev.Button == 0 && g.Contains(f.ev.Pos)
	}
	if res.Clicked || res.Pressed || res.RightClicked || res.DoubleClicked {
		f.w.again = true
	}
	return res
}

// Hovered reports whether the mouse is over r, without claiming it.
func (f *Frame) Hovered(r Rect) bool {
	in := f.w.input
	g := f.global(r)
	if f.clip != nil {
		g = g.Intersect(f.global(*f.clip))
	}
	return in.inside && g.Contains(in.pos)
}

// SetCursor shows cursor c while the mouse is over r.
func (f *Frame) SetCursor(r Rect, c Cursor) {
	f.w.cursors = append(f.w.cursors, cursorRegion{f.global(r), c})
}

// DragWindow makes r move the window when dragged, except where a region
// registered after it lies.
func (f *Frame) DragWindow(r Rect) {
	f.w.seq++
	f.w.drags = append(f.w.drags, dragArea{f.global(r), f.w.seq})
}

// Scroll shows content taller than r, scrolled with the wheel and a thin
// bar. content draws at the given rectangle, which is r.W wide and height
// tall, with its top moved by the scroll offset. It returns the offset.
func (f *Frame) Scroll(id string, r Rect, height float64, content func(area Rect)) float64 {
	off := f.w.scrolls[id]
	maxOff := math.Max(0, height-r.H)
	if f.ev.Kind == EvWheel && f.Hovered(r) {
		off -= f.ev.Wheel * 48
		f.w.again = true
	}
	off = math.Min(math.Max(off, 0), maxOff)
	f.w.scrolls[id] = off

	f.PushClip(r)
	saved := f.clip
	vis := Rect{r.X, r.Y + off, r.W, r.H}
	f.Translate(0, -off)
	f.clip = &vis
	content(Rect{r.X, r.Y, r.W, height})
	f.clip = saved
	f.Translate(0, off)
	f.PopClip()

	if maxOff > 0 {
		track := r.H
		thumb := math.Max(24, track*r.H/height)
		y := r.Y + (track-thumb)*off/maxOff
		f.FillRound(Rect{r.Right() - 5, y + 2, 3, thumb - 4}, 1.5, f.Theme.MutedForeground.WithAlpha(0.35))
	}
	return off
}

// ScrollTo sets the scroll offset of area id so that the band from top to
// bottom (in content coordinates, relative to the content's top) is
// visible in a viewport of the given height.
func (f *Frame) ScrollTo(id string, top, bottom, viewport float64) {
	off := f.w.scrolls[id]
	if top < off {
		off = top
	} else if bottom > off+viewport {
		off = bottom - viewport
	}
	if off != f.w.scrolls[id] {
		f.w.scrolls[id] = off
		f.w.again = true
	}
}

// ResetScroll scrolls area id back to the top.
func (f *Frame) ResetScroll(id string) { delete(f.w.scrolls, id) }
