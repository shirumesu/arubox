package ui

import (
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Field is the state of a text box. The view keeps it between frames.
type Field struct {
	Text        string
	Placeholder string
	Multiline   bool
	Password    bool
	ReadOnly    bool
	// Size is the font size in DIPs; 0 means 14.
	Size float64
	// Focus asks for the keyboard focus on the next frame.
	Focus bool
	// SelectAll asks for all text to be selected on the next frame.
	SelectAll bool
	// Changed is set when the user edits the text. The view clears it.
	Changed bool
	// Ghost makes the box take typing and the input method without showing
	// anything: the view draws the text itself, with TextRanges for the
	// caret and the selection, so what is typed shows as it will look. The
	// rectangle given to TextBox is then the caret's, where the input
	// method opens its window. A ghost box does not wrap.
	Ghost bool

	// synced is the text the native box was last given, and box that box:
	// a new Field, or one moved to another box, gives its text again.
	synced string
	box    uintptr
}

type edit struct {
	hwnd      uintptr
	w         *Window
	id        string
	field     *Field
	r         Rect
	placed    Bounds
	font      uintptr
	fontSize  float64
	fontScale float64
	bg        Color
	fg        Color
	brush     uintptr
	brushBG   Color
	used      bool
	syncing   bool
	multi     bool
	ghost     bool
	style     uint32
	// What the box was last told, since telling it again repaints it, and
	// frames run on every mouse move: the text flickered.
	told     bool
	readOnly bool
	cue      string
	// vis is what of r showed when place last put the box there, and rgn
	// that part in the box's pixels.
	vis *Rect
	rgn Bounds
}

const (
	enSetFocus  = 0x0100
	enKillFocus = 0x0200
)

// TextBox places a native text box over r, which the caller draws the
// frame of. bg is the opaque colour behind the text. It reports whether
// the user changed the text this frame.
func (f *Frame) TextBox(id string, r Rect, fld *Field, bg Color) bool {
	w := f.w
	e := w.edits[id]
	style := uint32(wsChild | wsTabStop | esAutoHScroll)
	if fld.Multiline {
		style = wsChild | wsTabStop | esMultiline | esAutoVScroll | esWantReturn | 0x00200000 // WS_VSCROLL
	}
	if fld.Password {
		style |= esPassword
	}
	if fld.Ghost {
		style = wsChild | wsTabStop | esAutoHScroll
		if fld.Multiline {
			style |= esMultiline | esAutoVScroll | esWantReturn
		}
	}
	if e != nil && e.style != style {
		e.release()
		delete(w.edits, id)
		e = nil
	}
	if e == nil {
		hwnd, _, _ := procCreateWindowExW.Call(0, utf16Ptr("EDIT"), 0, uintptr(style), 0, 0, 0, 0, w.hwnd, 0, instance, 0)
		if hwnd == 0 {
			return false
		}
		e = &edit{hwnd: hwnd, w: w, id: id, style: style, multi: fld.Multiline, ghost: fld.Ghost}
		w.edits[id] = e
		e.theme()
	}
	if fld.box != e.hwnd {
		// A new Field starts out as if in step with an empty box, which
		// left a box reused for it showing the previous text.
		fld.box = e.hwnd
		fld.synced = "\x00" // never a text, so the text is given
	}
	e.used = true
	e.field = fld
	e.bg = opaque(bg, f.Theme.Background)
	e.fg = f.Theme.Foreground
	if fld.Size == 0 {
		fld.Size = 14
	}
	if e.font == 0 || e.fontSize != fld.Size || e.fontScale != w.scale {
		e.setFont(fld.Size)
	}
	if fld.Text != fld.synced {
		e.syncing = true
		procSetWindowTextW.Call(e.hwnd, utf16Ptr(fld.Text))
		e.syncing = false
		fld.synced = fld.Text
		n := len(windowsUTF16(fld.Text))
		procSendMessageW.Call(e.hwnd, emSetSel, uintptr(n), uintptr(n))
	}
	if !e.told || e.readOnly != fld.ReadOnly {
		procSendMessageW.Call(e.hwnd, emSetReadOnly, boolArg(fld.ReadOnly), 0)
		e.readOnly = fld.ReadOnly
	}
	cue := fld.Placeholder
	if fld.Multiline {
		cue = "" // a multi-line box shows no placeholder
	}
	if !e.told || e.cue != cue {
		procSendMessageW.Call(e.hwnd, emSetCueBanner, 0, utf16Ptr(cue))
		e.cue = cue
	}
	e.told = true
	g := f.global(r)
	var vis *Rect
	if f.clip != nil {
		// A text box is a child window, which drawing clips cannot reach:
		// one scrolled partly out of its area is cut to the part inside by
		// a window region, and one scrolled out of it is hidden.
		v := f.global(*f.clip)
		if g.Intersect(v).Empty() {
			e.hide()
			return f.ev.Kind == EvTextChanged && f.ev.ID == id
		}
		vis = &v
	}
	e.place(g, vis, w)
	if fld.Focus {
		fld.Focus = false
		procSetFocus.Call(e.hwnd)
		w.focusEdit = id
	}
	if fld.SelectAll {
		fld.SelectAll = false
		procSendMessageW.Call(e.hwnd, emSetSel, 0, ^uintptr(0))
	}
	if f.ev.Kind == EvKeyDown && f.ev.ID == id && f.ev.Key == 'A' && f.ev.Mods == ModifierCtrl {
		procSendMessageW.Call(e.hwnd, emSetSel, 0, ^uintptr(0))
		f.TakeKey()
	}
	changed := f.ev.Kind == EvTextChanged && f.ev.ID == id
	return changed
}

// FocusedBox returns the id of the text box with the focus, or "".
func (f *Frame) FocusedBox() string {
	fh, _, _ := procGetFocus.Call()
	for id, e := range f.w.edits {
		if e.hwnd == fh {
			return id
		}
	}
	return ""
}

// Blur takes the keyboard focus off the text boxes.
func (f *Frame) Blur() {
	procSetFocus.Call(f.w.hwnd)
	f.w.focusEdit = ""
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func windowsUTF16(s string) []uint16 {
	u, _ := windows.UTF16FromString(s)
	if len(u) > 0 {
		return u[:len(u)-1]
	}
	return u
}

// opaque blends a translucent colour over a background.
func opaque(c, under Color) Color {
	if c.A >= 1 {
		return c
	}
	return Color{R: under.R + (c.R-under.R)*c.A, G: under.G + (c.G-under.G)*c.A, B: under.B + (c.B-under.B)*c.A, A: 1}
}

func (e *edit) setFont(size float64) {
	if e.font != 0 {
		procDeleteObject.Call(e.font)
	}
	h := -int32(math.Round(size * e.w.scale))
	e.font, _, _ = procCreateFontW.Call(uintptr(h), 0, 0, 0, 400, 0, 0, 0, 1 /* DEFAULT_CHARSET */, 0, 0, 5 /* CLEARTYPE_QUALITY */, 0,
		utf16Ptr("Microsoft YaHei UI"))
	e.fontSize, e.fontScale = size, e.w.scale
	procSendMessageW.Call(e.hwnd, wmSetFont, e.font, 1)
	procSendMessageW.Call(e.hwnd, emSetMargins, ecLeftMargin|ecRightMargin, 0)
	e.placed = Bounds{}
}

// place moves the box to r, in window DIPs, showing only its part inside
// vis when that is given. A single-line box is as tall as its text and
// centred in r.
func (e *edit) place(r Rect, vis *Rect, w *Window) {
	e.r, e.vis = r, vis
	s := w.scale
	b := Bounds{int(math.Round(r.X * s)), int(math.Round(r.Y * s)), int(math.Round(r.W * s)), int(math.Round(r.H * s))}
	if e.ghost {
		b.W, b.H = 0, 0
	} else if !e.multi {
		lh := int(math.Ceil(e.fontSize * s * 1.42))
		if lh < b.H {
			b.Y += (b.H - lh) / 2
			b.H = lh
		}
	}
	if b != e.placed {
		e.placed = b
		procSetWindowPos.Call(e.hwnd, 0, uintptr(b.X), uintptr(b.Y), uintptr(b.W), uintptr(b.H), swpNoZOrder|swpNoActivate|swpShowWindow)
	} else if r, _, _ := procIsWindowVisible.Call(e.hwnd); r == 0 {
		procShowWindow.Call(e.hwnd, swShowNoActivate)
	}
	// The region, in the box's own pixels; the zero Bounds means all of it.
	var rgn Bounds
	if vis != nil {
		x0, y0 := max(b.X, int(math.Round(vis.X*s))), max(b.Y, int(math.Round(vis.Y*s)))
		x1 := min(b.X+b.W, int(math.Round(vis.Right()*s)))
		y1 := min(b.Y+b.H, int(math.Round(vis.Bottom()*s)))
		if x0 > b.X || y0 > b.Y || x1 < b.X+b.W || y1 < b.Y+b.H {
			rgn = Bounds{x0 - b.X, y0 - b.Y, max(0, x1-x0), max(0, y1-y0)}
		}
	}
	if rgn != e.rgn {
		e.rgn = rgn
		h := uintptr(0)
		if rgn != (Bounds{}) {
			// The window owns the region from here on.
			h, _, _ = procCreateRectRgn.Call(uintptr(rgn.X), uintptr(rgn.Y), uintptr(rgn.X+rgn.W), uintptr(rgn.Y+rgn.H))
		}
		procSetWindowRgn.Call(e.hwnd, h, 1)
	}
}

func (e *edit) hide() {
	if e.hwnd != 0 {
		procShowWindow.Call(e.hwnd, swHide)
		e.placed = Bounds{}
	}
}

func (e *edit) changed() {
	if e.syncing || e.field == nil {
		return
	}
	n, _, _ := procGetWindowTextLengthW.Call(e.hwnd)
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(e.hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	t := windows.UTF16ToString(buf)
	e.field.Text, e.field.synced = t, t
	e.field.Changed = true
}

func (e *edit) theme() {
	name := "Explorer"
	if DarkMode() {
		name = "DarkMode_Explorer"
	}
	procSetWindowTheme.Call(e.hwnd, utf16Ptr(name), 0)
}

func (e *edit) colors(hdc uintptr) uintptr {
	procSetTextColor.Call(hdc, colorref(e.fg.R, e.fg.G, e.fg.B))
	procSetBkColor.Call(hdc, colorref(e.bg.R, e.bg.G, e.bg.B))
	if e.brush == 0 || e.brushBG != e.bg {
		if e.brush != 0 {
			procDeleteObject.Call(e.brush)
		}
		e.brush, _, _ = procCreateSolidBrush.Call(colorref(e.bg.R, e.bg.G, e.bg.B))
		e.brushBG = e.bg
	}
	return e.brush
}

func (e *edit) release() {
	if e.hwnd != 0 {
		procDestroyWindow.Call(e.hwnd)
		e.hwnd = 0
	}
	if e.font != 0 {
		procDeleteObject.Call(e.font)
		e.font = 0
	}
	if e.brush != 0 {
		procDeleteObject.Call(e.brush)
		e.brush = 0
	}
}
