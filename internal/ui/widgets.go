package ui

import (
	"math"
	"unsafe"

	"github.com/shirumesu/arubox/internal/design"
)

// Icon glyphs of Segoe Fluent Icons (Windows 11), which Segoe MDL2 Assets
// (Windows 10) mostly shares.
const (
	IconSearch       = ''
	IconSettings     = ''
	IconKeyboard     = ''
	IconApps         = ''
	IconInfo         = ''
	IconClose        = ''
	IconCheck        = ''
	IconCopy         = ''
	IconPin          = ''
	IconUnpin        = ''
	IconSpeaker      = ''
	IconStop         = ''
	IconSwap         = ''
	IconTranslate    = ''
	IconCamera       = ''
	IconCrop         = ''
	IconPen          = ''
	IconText         = ''
	IconArrow        = '' // a right arrow
	IconRect         = ''
	IconEllipse      = ''
	IconMosaic       = ''
	IconUndo         = ''
	IconSave         = ''
	IconDocument     = ''
	IconFolder       = ''
	IconCalculator   = ''
	IconGlobe        = ''
	IconClock        = ''
	IconMoney        = ''
	IconRefresh      = ''
	IconDownload     = ''
	IconChevronDown  = ''
	IconChevronRight = ''
	IconAdd          = ''
	IconDelete       = ''
	IconWarning      = ''
	IconLink         = ''
	IconDesktop      = ''
	IconRead         = '' // OCR / "read text"
	IconHistory      = ''
	IconMore         = ''
)

var iconFamily = func() string {
	// Segoe Fluent Icons ships with Windows 11; Windows 10 has MDL2.
	return "Segoe Fluent Icons"
}()

// Icon draws a glyph centred in r.
func (f *Frame) Icon(r Rect, glyph rune, size float64, col Color) {
	f.Text(r, string(glyph), Text{Family: iconFamily, Size: size, Color: col, Align: AlignCenter, Middle: true})
}

// ButtonKind is a button's look.
type ButtonKind int

// Button kinds.
const (
	ButtonOutline ButtonKind = iota
	ButtonPrimary
	ButtonGhost
	ButtonDestructive
	ButtonSecondary
)

// Button is a push button.
type Button struct {
	Label    string
	Icon     rune
	Kind     ButtonKind
	Disabled bool
	// Small uses a smaller font and padding.
	Small bool
}

// Width returns the width the button needs.
func (b Button) Width() float64 {
	size, pad := 13.0, 12.0
	if b.Small {
		size, pad = 12.5, 10
	}
	w := 0.0
	if b.Label != "" {
		w, _ = MeasureText(b.Label, Text{Size: size, Weight: Medium}, 0)
	}
	if b.Icon != 0 {
		if b.Label != "" {
			w += 6
		}
		w += 16
	}
	if b.Label == "" {
		return 32
	}
	return math.Ceil(w + 2*pad)
}

// Button draws a button in r and reports whether it was clicked.
func (f *Frame) Button(id string, r Rect, b Button) bool {
	res := Response{}
	if !b.Disabled {
		res = f.Interact(id, r)
		f.SetCursor(r, CursorHand)
	}
	t := f.Theme
	var bg, fg, border Color
	switch b.Kind {
	case ButtonPrimary:
		bg, fg = t.Primary, t.PrimaryForeground
		if res.Hovered {
			bg = bg.Mix(t.Foreground, 0.08)
		}
	case ButtonDestructive:
		bg, fg = t.Destructive, design.Color{R: 1, G: 1, B: 1, A: 1}
		if res.Hovered {
			bg = bg.Mix(t.Foreground, 0.08)
		}
	case ButtonSecondary:
		bg, fg = t.Secondary, t.SecondaryForeground
		if res.Hovered {
			bg = t.Accent
		}
	case ButtonGhost:
		fg = t.Foreground
		if res.Hovered {
			bg = t.Accent
		}
	default:
		bg, fg, border = t.Card, t.Foreground, t.Input
		if res.Hovered {
			bg = t.Accent
		}
	}
	if res.Held {
		bg = bg.Mix(t.Foreground, 0.06)
	}
	if b.Disabled {
		bg, fg = bg.WithAlpha(bg.A*0.5), fg.WithAlpha(0.45)
	}
	radius := design.Radius - 3
	f.FillRound(r, radius, bg)
	if border.A > 0 {
		f.StrokeRound(r, radius, 1, border)
	}
	size := 13.0
	if b.Small {
		size = 12.5
	}
	inner := r
	switch {
	case b.Icon != 0 && b.Label != "":
		tw, _ := MeasureText(b.Label, Text{Size: size, Weight: Medium}, 0)
		total := 16 + 6 + tw
		x := r.X + (r.W-total)/2
		f.Icon(Rect{x, r.Y, 16, r.H}, b.Icon, 14, fg)
		inner = Rect{x + 22, r.Y, tw + 2, r.H}
		f.Text(inner, b.Label, Text{Size: size, Weight: Medium, Color: fg, Middle: true})
	case b.Icon != 0:
		f.Icon(r, b.Icon, 15, fg)
	default:
		f.Text(inner, b.Label, Text{Size: size, Weight: Medium, Color: fg, Align: AlignCenter, Middle: true})
	}
	return res.Clicked
}

// Switch draws an on/off switch in r (best 36×20) and flips *on when
// clicked. It reports whether it flipped.
func (f *Frame) Switch(id string, r Rect, on *bool, disabled bool) bool {
	r = r.Center(36, 20)
	res := Response{}
	if !disabled {
		res = f.Interact(id, r.Inset(-4))
		f.SetCursor(r, CursorHand)
	}
	t := f.Theme
	track := t.Input
	if *on {
		track = t.Primary
	}
	if res.Hovered {
		track = track.Mix(t.Foreground, 0.08)
	}
	if disabled {
		track = track.WithAlpha(track.A * 0.5)
	}
	f.FillRound(r, 10, track)
	x := r.X + 2
	if *on {
		x = r.Right() - 18
	}
	knob := design.Color{R: 1, G: 1, B: 1, A: 1}
	if !*on && DarkMode() {
		knob = t.Foreground.WithAlpha(0.85)
	}
	f.FillEllipse(Rect{x, r.Y + 2, 16, 16}, knob)
	if res.Clicked {
		*on = !*on
		return true
	}
	return false
}

// Checkbox draws a check box and its label in r and flips *on when
// clicked. It reports whether it flipped.
func (f *Frame) Checkbox(id string, r Rect, label string, on *bool) bool {
	res := f.Interact(id, r)
	f.SetCursor(r, CursorHand)
	t := f.Theme
	box := Rect{r.X, r.Y + (r.H-16)/2, 16, 16}
	if *on {
		f.FillRound(box, 4, t.Primary)
		f.Icon(box, IconCheck, 11, t.PrimaryForeground)
	} else {
		bg := t.Card
		if res.Hovered {
			bg = t.Accent
		}
		f.FillRound(box, 4, bg)
		f.StrokeRound(box, 4, 1, t.Input.WithAlpha(math.Max(t.Input.A, 0.5)))
	}
	f.Text(Rect{r.X + 24, r.Y, r.W - 24, r.H}, label, Text{Size: 13, Color: t.Foreground, Middle: true})
	if res.Clicked {
		*on = !*on
		return true
	}
	return false
}

// Select draws a drop-down showing options[*index] and opens a menu of
// the options when clicked. It reports whether the choice changed.
func (f *Frame) Select(id string, r Rect, options []string, index *int) bool {
	res := f.Interact(id, r)
	f.SetCursor(r, CursorHand)
	t := f.Theme
	bg := t.Card
	if res.Hovered {
		bg = t.Accent
	}
	f.FillRound(r, design.Radius-3, bg)
	f.StrokeRound(r, design.Radius-3, 1, t.Input)
	label := ""
	if *index >= 0 && *index < len(options) {
		label = options[*index]
	}
	f.Text(Rect{r.X + 10, r.Y, r.W - 34, r.H}, label, Text{Size: 13, Color: t.Foreground, Middle: true})
	f.Icon(Rect{r.Right() - 26, r.Y, 18, r.H}, IconChevronDown, 10, t.MutedForeground)
	if !res.Clicked {
		return false
	}
	items := make([]MenuItem, len(options))
	chosen := -1
	for i, o := range options {
		items[i] = MenuItem{Label: o, Checked: i == *index, Action: func() { chosen = i }}
	}
	g := f.global(r)
	p := point{int32(g.X * f.w.scale), int32(g.Bottom()*f.w.scale + 2)}
	procClientToScreen.Call(f.w.hwnd, uintptr(unsafe.Pointer(&p)))
	f.w.input.down, f.w.input.active = -1, ""
	PopupMenu(f.w.hwnd, int(p.X), int(p.Y), items)
	if chosen >= 0 && chosen != *index {
		*index = chosen
		f.Invalidate()
		return true
	}
	return false
}

// Menu opens a native menu under r, as a "more" button does.
func (f *Frame) Menu(r Rect, items []MenuItem) {
	g := f.global(r)
	p := point{int32(g.X * f.w.scale), int32(g.Bottom()*f.w.scale + 2)}
	procClientToScreen.Call(f.w.hwnd, uintptr(unsafe.Pointer(&p)))
	f.w.input.down, f.w.input.active = -1, ""
	PopupMenu(f.w.hwnd, int(p.X), int(p.Y), items)
	f.Invalidate()
}

// Badge draws a small pill label and returns its width. tone is the text
// colour; the fill is a faint version of it.
func (f *Frame) Badge(x, y float64, label string, tone Color) float64 {
	w, _ := MeasureText(label, Text{Size: 11.5, Weight: Medium}, 0)
	r := Rect{x, y, math.Ceil(w) + 14, 20}
	f.FillRound(r, 10, tone.WithAlpha(0.14))
	f.Text(r, label, Text{Size: 11.5, Weight: Medium, Color: tone, Align: AlignCenter, Middle: true})
	return r.W
}

// Progress draws a bar filled to fraction p of r.
func (f *Frame) Progress(r Rect, p float64) {
	t := f.Theme
	f.FillRound(r, r.H/2, t.Secondary)
	p = math.Min(math.Max(p, 0), 1)
	if p > 0 {
		f.FillRound(Rect{r.X, r.Y, math.Max(r.H, r.W*p), r.H}, r.H/2, t.Primary)
	}
}

// Separator draws a one-pixel line across r's top.
func (f *Frame) Separator(r Rect) {
	f.Fill(Rect{r.X, r.Y, r.W, 1 / f.Scale}, f.Theme.Border)
}

// Label draws one line of text in r.
func (f *Frame) Label(r Rect, s string, size float64, col Color) {
	f.Text(r, s, Text{Size: size, Color: col, Middle: true})
}

// Row is one setting: a title and description on the left and a control
// on the right.
type Row struct {
	Title       string
	Description string
	// Note is shown under the description in the destructive colour.
	Note string
	// ControlW is the width the control needs.
	ControlW float64
	// ControlH is the control's height; 0 means 32.
	ControlH float64
	// Control draws the control in its rectangle.
	Control func(r Rect)
	// Extra draws more below the text, Height DIPs tall, across the row.
	ExtraH float64
	Extra  func(r Rect)
	// Badge, when set, is drawn after the title.
	Badge     string
	BadgeTone Color
}

func (row Row) height(w float64) float64 {
	textW := w - 32
	if row.Control != nil {
		textW -= row.ControlW + 16
	}
	h := 20.0
	if row.Description != "" {
		_, dh := MeasureText(row.Description, Text{Size: 13, Wrap: true}, textW)
		h += 2 + dh
	}
	if row.Note != "" {
		_, nh := MeasureText(row.Note, Text{Size: 13, Wrap: true}, textW)
		h += 2 + nh
	}
	h = math.Max(h+24, 56)
	if row.ExtraH > 0 {
		h += row.ExtraH
	}
	return h
}

// Section draws a titled card of rows from y in the column x..x+w and
// returns the y below it.
func (f *Frame) Section(x, y, w float64, title string, rows []Row) float64 {
	t := f.Theme
	if title != "" {
		f.Text(Rect{x + 4, y, w, 18}, title, Text{Size: 12, Weight: Medium, Color: t.MutedForeground, Middle: true})
		y += 26
	}
	heights := make([]float64, len(rows))
	total := 0.0
	for i, r := range rows {
		heights[i] = r.height(w)
		total += heights[i]
	}
	card := Rect{x, y, w, total}
	f.FillRound(card, design.Radius, t.Card)
	f.StrokeRound(card, design.Radius, 1, t.Border)
	ry := y
	for i, row := range rows {
		if i > 0 {
			f.Fill(Rect{x + 1, ry, w - 2, 1 / f.Scale}, t.Border)
		}
		h := heights[i]
		textW := w - 32
		if row.Control != nil {
			textW -= row.ControlW + 16
		}
		body := h - row.ExtraH
		// The text block, centred in the row above any extra content.
		blockH := 20.0
		var dh, nh float64
		if row.Description != "" {
			_, dh = MeasureText(row.Description, Text{Size: 13, Wrap: true}, textW)
			blockH += 2 + dh
		}
		if row.Note != "" {
			_, nh = MeasureText(row.Note, Text{Size: 13, Wrap: true}, textW)
			blockH += 2 + nh
		}
		ty := ry + (body-blockH)/2
		f.Text(Rect{x + 16, ty, textW, 20}, row.Title, Text{Size: 14, Weight: Medium, Color: t.Foreground, Middle: true})
		if row.Badge != "" {
			tw, _ := MeasureText(row.Title, Text{Size: 14, Weight: Medium}, 0)
			f.Badge(x+16+tw+8, ty, row.Badge, row.BadgeTone)
		}
		ty += 22
		if row.Description != "" {
			f.Text(Rect{x + 16, ty, textW, dh}, row.Description, Text{Size: 13, Wrap: true, Color: t.MutedForeground})
			ty += dh + 2
		}
		if row.Note != "" {
			f.Text(Rect{x + 16, ty, textW, nh}, row.Note, Text{Size: 13, Wrap: true, Color: t.Destructive})
		}
		if row.Control != nil {
			ch := row.ControlH
			if ch == 0 {
				ch = 32
			}
			row.Control(Rect{x + w - 16 - row.ControlW, ry + (body-ch)/2, row.ControlW, ch})
		}
		if row.Extra != nil {
			row.Extra(Rect{x + 16, ry + body - 8, w - 32, row.ExtraH})
		}
		ry += h
	}
	return y + total
}

// Empty draws a dashed placeholder box with an icon, title and text from
// y and returns the y below it.
func (f *Frame) Empty(x, y, w float64, icon rune, title, text string) float64 {
	t := f.Theme
	_, th := MeasureText(text, Text{Size: 13, Wrap: true, Align: AlignCenter}, math.Min(w-48, 380))
	h := 48 + 32 + 10 + 20 + 6 + th + 48
	r := Rect{x, y, w, h}
	f.Stroke(r, 1, t.Border, true)
	f.Icon(Rect{x, y + 48, w, 32}, icon, 28, t.MutedForeground)
	f.Text(Rect{x, y + 90, w, 20}, title, Text{Size: 14, Weight: Medium, Color: t.Foreground, Align: AlignCenter, Middle: true})
	tw := math.Min(w-48, 380)
	f.Text(Rect{x + (w-tw)/2, y + 116, tw, th}, text, Text{Size: 13, Wrap: true, Align: AlignCenter, Color: t.MutedForeground})
	return y + h
}

// Dialog draws a modal dialog box of the given size centred in the
// window over a dimmed backdrop, which takes every click outside it. body
// draws the box's content. It reports whether the backdrop was clicked.
func (f *Frame) Dialog(id string, w, h float64, body func(r Rect)) (dismissed bool) {
	full := f.Bounds()
	res := f.Interact(id+".backdrop", full)
	f.Fill(full, design.Color{A: 0.45})
	box := full.Center(math.Min(w, full.W-32), math.Min(h, full.H-32))
	f.Interact(id+".box", box)
	t := f.Theme
	f.FillRound(box, design.Radius+2, t.Popover)
	f.StrokeRound(box, design.Radius+2, 1, t.Border)
	body(box.Inset(24))
	return res.Clicked
}

// Toast is a short message shown at the bottom of a window for a while.
type Toast struct {
	text  string
	error bool
	until int64
}

// Show sets the message; it hides after ms milliseconds.
func (t *Toast) Show(w *Window, text string, isError bool, ms int) {
	t.text, t.error = text, isError
	t.until++
	n := t.until
	w.After(ms, func() {
		if t.until == n {
			t.text = ""
			w.Invalidate()
		}
	})
	w.Invalidate()
}

// Draw paints the toast, if any, at the bottom of the window.
func (t *Toast) Draw(f *Frame) {
	if t.text == "" {
		return
	}
	th := f.Theme
	w, _ := MeasureText(t.text, Text{Size: 13}, 0)
	fw, fh := f.Size()
	r := Rect{(fw - w - 32) / 2, fh - 64, w + 32, 38}
	f.FillRound(r, design.Radius, th.Popover)
	f.StrokeRound(r, design.Radius, 1, th.Border)
	col := th.Foreground
	if t.error {
		col = th.Destructive
	}
	f.Text(r, t.text, Text{Size: 13, Color: col, Align: AlignCenter, Middle: true})
}
