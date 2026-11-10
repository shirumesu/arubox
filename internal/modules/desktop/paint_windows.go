package desktop

import (
	"math"
	"path/filepath"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/platform"
)

var (
	white  = design.Color{R: 1, G: 1, B: 1, A: 1}
	shadow = design.Color{A: 0.6}
)

// boxColors are the backgrounds a box can have, before its opacity.
var boxColors = map[string]design.Color{
	"":       {R: 0.09, G: 0.09, B: 0.1},
	"blue":   {R: 0.09, G: 0.17, B: 0.33},
	"teal":   {R: 0.05, G: 0.24, B: 0.26},
	"green":  {R: 0.09, G: 0.23, B: 0.13},
	"purple": {R: 0.21, G: 0.12, B: 0.32},
	"red":    {R: 0.32, G: 0.09, B: 0.1},
	"orange": {R: 0.35, G: 0.18, B: 0.06},
}

// boxColorNames lists the colours in the order the menu shows them.
var boxColorNames = []struct{ key, name string }{
	{"", "默认"}, {"blue", "蓝"}, {"teal", "青"}, {"green", "绿"}, {"purple", "紫"}, {"red", "红"}, {"orange", "橙"},
}

// Paint draws the part of the screen inside dirty.
func (v *view) Paint(c *platform.Canvas, dirty platform.Box) {
	if v.hidden {
		return
	}
	var expanded *cell
	for _, cl := range v.loose {
		if intersects(cl.rect, dirty) {
			if v.drawCell(c, cl, false) {
				expanded = cl
			}
		}
	}
	for _, g := range v.boxes {
		if intersects(g.rect, dirty) {
			if e := v.drawBox(c, g, dirty); e != nil {
				expanded = e
			}
		}
	}
	// The selected item's whole name goes over its neighbours.
	if expanded != nil {
		if expanded.box != nil {
			c.PushClip(expanded.box.content)
		}
		v.drawCell(c, expanded, true)
		if expanded.box != nil {
			c.PopClip()
		}
	}
	if v.band.Width > 0 && v.band.Height > 0 {
		c.FillRect(v.band, v.tokens.Primary.WithAlpha(0.16))
		c.StrokeRoundRect(platform.Box{X: v.band.X + 0.5, Y: v.band.Y + 0.5, Width: v.band.Width - 1, Height: v.band.Height - 1}, 0, 1, v.tokens.Primary.WithAlpha(0.85))
	}
}

// expandsName reports whether a cell shows its whole name: the only
// selected item does, as on File Explorer's desktop.
func (v *view) expandsName(cl *cell) bool {
	return len(v.selected) == 1 && v.selected[cl.item.Key] && v.selBox == boxID(cl) && v.editing == nil
}

func boxID(cl *cell) string {
	if cl.box == nil {
		return ""
	}
	return cl.box.box.ID
}

// drawCell draws an item. A selected item whose name is shorter in its
// cell than in full is drawn again, expanded, over the others; drawCell
// reports when it needs that.
func (v *view) drawCell(c *platform.Canvas, cl *cell, expanded bool) bool {
	m, r := cl.m, cl.rect
	selected := v.selected[cl.item.Key] && v.selBox == boxID(cl)
	hovered := v.hover.cell != nil && v.hover.cell.item.Key == cl.item.Key && boxID(v.hover.cell) == boxID(cl)
	iconY := r.Y + m.iconTop
	labelY := iconY + m.icon + m.labelGap
	lines := 2
	if expanded {
		lines = 8
	}
	label := v.text(cl.item.Name, m.labelW, m.font, m.line, lines, false)
	full := label
	wantsExpand := false
	if !expanded && v.expandsName(cl) {
		full = v.text(cl.item.Name, m.labelW, m.font, m.line, 8, false)
		wantsExpand = full != nil && label != nil && full.Height > label.Height+0.5
	}
	labelH := m.line
	if label != nil {
		labelH = label.Height
	}
	hl := platform.Box{X: r.X + 1, Y: r.Y + 1, Width: r.Width - 2, Height: labelY + labelH + m.pad/2 - r.Y - 1}
	radius := math.Max(2, m.radius/2)
	switch {
	case selected:
		c.FillRoundRect(hl, radius, v.tokens.Primary.WithAlpha(0.32))
		c.StrokeRoundRect(platform.Box{X: hl.X + 0.5, Y: hl.Y + 0.5, Width: hl.Width - 1, Height: hl.Height - 1}, radius, 1, v.tokens.Primary.WithAlpha(0.75))
	case hovered:
		c.FillRoundRect(hl, radius, white.WithAlpha(0.12))
	}
	if b := v.icon(c, cl.item.Key, int(m.icon)); b != nil {
		// Fit the picture in the icon's square, keeping its shape.
		f := math.Min(m.icon/float64(b.Width), m.icon/float64(b.Height))
		w, h := float64(b.Width)*f, float64(b.Height)*f
		c.DrawBitmap(b, platform.Box{X: r.X + (r.Width-w)/2, Y: iconY + (m.icon - h), Width: w, Height: h}, 1)
	}
	if label != nil && (v.editing == nil || v.editing.key != cl.item.Key) {
		x := r.X + (r.Width-m.labelW)/2
		c.DrawText(label, x+1, labelY+1, shadow)
		c.DrawText(label, x, labelY, white)
	}
	return wantsExpand
}

// drawBox draws a box and the items in it, and returns an item of it to be
// drawn again expanded.
func (v *view) drawBox(c *platform.Canvas, g *boxGeo, dirty platform.Box) *cell {
	m, b := g.m, g.box
	bg := boxColors[b.Color]
	bg.A = float64(v.settings.Opacity) / 100
	c.FillRoundRect(g.rect, m.radius, bg)
	border, width := white.WithAlpha(0.1), 1.0
	switch {
	case v.dropBox == b.ID:
		border, width = v.tokens.Primary.WithAlpha(0.9), math.Round(2*m.scale)
	case v.hover.box == g:
		border = white.WithAlpha(0.2)
	}
	in := width / 2
	c.StrokeRoundRect(platform.Box{X: g.rect.X + in, Y: g.rect.Y + in, Width: g.rect.Width - 2*in, Height: g.rect.Height - 2*in}, m.radius, width, border)

	title := b.Title
	left := g.title.X + m.pad
	if p := v.portals[b.ID]; p != nil && len(p.back) > 0 {
		title = filepath.Base(p.path)
		if arrow := v.text("‹", m.title, m.titleFont*1.5, m.title, 1, true); arrow != nil {
			c.DrawText(arrow, g.title.X, g.title.Y+(m.title-arrow.Height)/2-m.scale, white.WithAlpha(0.9))
		}
		left = g.title.X + m.title
	}
	if t := v.text(title, g.title.Width-2*(left-g.title.X), m.titleFont, m.line, 1, true); t != nil && (v.editing == nil || v.editing.key != "" || v.editing.box != b.ID) {
		y := g.title.Y + (m.title-t.Height)/2
		c.DrawText(t, left+1, y+1, shadow)
		c.DrawText(t, left, y, white.WithAlpha(0.95))
	}
	if g.rect.Height <= m.title+1 {
		return nil
	}
	c.FillRect(platform.Box{X: g.rect.X + m.pad, Y: g.title.Y + m.title - 1, Width: g.rect.Width - 2*m.pad, Height: 1}, white.WithAlpha(0.08))
	var expand *cell
	c.PushClip(g.content)
	for _, cl := range g.cells {
		if intersects(cl.rect, g.content) && intersects(cl.rect, dirty) {
			if v.drawCell(c, cl, false) {
				expand = cl
			}
		}
	}
	if len(g.cells) == 0 {
		hint := "把图标拖到这里"
		if b.Folder != "" {
			hint = "文件夹是空的"
		}
		if t := v.text(hint, g.content.Width, m.font, m.line, 2, false); t != nil {
			c.DrawText(t, g.content.X, g.content.Y+(g.content.Height-t.Height)/2, white.WithAlpha(0.5))
		}
	}
	c.PopClip()
	visible := g.full.Height - m.title - m.pad
	if v.hover.box == g && g.contentH > visible+0.5 && g.content.Height > 0 {
		track := g.content.Height
		h := math.Max(m.cellH/3, track*visible/g.contentH)
		y := g.content.Y + (track-h)*g.scroll/(g.contentH-visible)
		c.FillRoundRect(platform.Box{X: g.rect.X + g.rect.Width - m.scrollWidth - m.pad/2, Y: y, Width: m.scrollWidth, Height: h}, m.scrollWidth/2, white.WithAlpha(0.35))
	}
	return expand
}
