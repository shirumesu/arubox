package translate

import (
	"math"
	"time"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/ui"
)

// The small controls the popup is made of, drawn the way the old page's
// ghost buttons and plain-text menus looked.

const (
	iconButtonSize = 28.0
	pickerHeight   = 28.0
	pickerFont     = 12.0
)

// iconButton draws a square ghost button with a glyph and reports whether
// it was clicked. active tints it with the primary colour, as a pressed
// toggle.
func iconButton(f *ui.Frame, id string, r ui.Rect, glyph rune, active bool) bool {
	res := f.Interact(id, r)
	f.SetCursor(r, ui.CursorHand)
	t := f.Theme
	fg := t.MutedForeground
	if res.Hovered {
		f.FillRound(r, design.Radius-4, t.Accent)
		fg = t.AccentForeground
	}
	if active {
		fg = t.Primary
	}
	f.Icon(r, glyph, 14, fg)
	return res.Clicked
}

// pickerWidth is how wide a picker showing label is.
func pickerWidth(label string) float64 {
	w, _ := ui.MeasureText(label, ui.Text{Size: pickerFont}, 0)
	return math.Ceil(w) + 6 + 22
}

// picker draws a menu as plain text with a chevron, starting at x, y, and
// opens the native menu of opts when clicked. It returns its width and
// the value picked this frame, if any.
func picker(f *ui.Frame, id string, x, y float64, label string, opts []option, value string) (width float64, picked string, ok bool) {
	w := pickerWidth(label)
	r := ui.Rect{X: x, Y: y, W: w, H: pickerHeight}
	res := f.Interact(id, r)
	f.SetCursor(r, ui.CursorHand)
	t := f.Theme
	fg := t.MutedForeground
	if res.Hovered {
		f.FillRound(r, design.Radius-4, t.Accent)
		fg = t.AccentForeground
	}
	f.Text(ui.Rect{X: r.X + 6, Y: r.Y, W: w - 6 - 18, H: r.H}, label, ui.Text{Size: pickerFont, Color: fg, Middle: true})
	f.Icon(ui.Rect{X: r.Right() - 20, Y: r.Y, W: 14, H: r.H}, ui.IconChevronDown, 9, fg)
	if !res.Clicked {
		return w, "", false
	}
	current := optionIndex(opts, value)
	items := make([]ui.MenuItem, len(opts))
	for i, o := range opts {
		items[i] = ui.MenuItem{Label: o.label, Checked: i == current, Action: func() { picked, ok = o.value, true }}
	}
	f.Menu(r, items)
	if ok && current >= 0 && opts[current].value == picked {
		ok = false
	}
	return w, picked, ok
}

// spinner draws a ring of dots that turns while loading. The caller asks
// for the frames that turn it.
func spinner(f *ui.Frame, r ui.Rect, col ui.Color) {
	const dots = 8
	step := int(time.Now().UnixMilli()/90) % dots
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	radius := math.Min(r.W, r.H)/2 - 1.5
	for i := range dots {
		a := float64(i) * 2 * math.Pi / dots
		age := (step - i + dots) % dots
		alpha := 1 - float64(age)/dots*0.85
		x, y := cx+radius*math.Cos(a), cy+radius*math.Sin(a)
		f.FillEllipse(ui.Rect{X: x - 1.25, Y: y - 1.25, W: 2.5, H: 2.5}, col.WithAlpha(alpha))
	}
}
