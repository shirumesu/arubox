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
	expanded := v.expandedCell()
	for _, cl := range v.loose {
		if cl != expanded && intersects(cl.rect, dirty) {
			v.drawCell(c, cl, false)
		}
	}
	for _, g := range v.boxes {
		if intersects(g.rect, dirty) {
			v.drawBox(c, g, dirty, expanded)
		}
	}
	// The selected item's whole name goes over its neighbours, so it is
	// drawn last, and only then, wherever its name reaches.
	if expanded != nil && intersects(v.cellExtent(expanded), dirty) {
		if g := expanded.box; g == nil {
			v.drawCell(c, expanded, true)
		} else if g.rect.Height > g.m.title+1 && intersects(expanded.rect, g.content) {
			c.PushClip(g.content)
			v.drawCell(c, expanded, true)
			c.PopClip()
		}
	}
	if v.band.Width > 0 && v.band.Height > 0 {
		c.FillRect(v.band, design.Dark.Primary.WithAlpha(0.16))
		c.StrokeRoundRect(platform.Box{X: v.band.X + 0.5, Y: v.band.Y + 0.5, Width: v.band.Width - 1, Height: v.band.Height - 1}, 0, 1, design.Dark.Primary.WithAlpha(0.85))
	}
}

// expandedCell returns the item that shows its whole name, if its name is
// longer than its cell shows: the only selected item does, as on File
// Explorer's desktop.
func (v *view) expandedCell() *cell {
	if len(v.selected) != 1 || v.editing != nil {
		return nil
	}
	for _, cl := range v.containerCells(v.selBox) {
		if !v.selected[cl.item.Key] {
			continue
		}
		m := cl.m
		label := v.text(cl.item.Name, m.labelW, m.font, m.line, 2, false)
		full := v.text(cl.item.Name, m.labelW, m.font, m.line, 8, false)
		if label != nil && full != nil && full.Height > label.Height+0.5 {
			return cl
		}
		return nil
	}
	return nil
}

// cellExtent is the space an item takes with its whole name showing,
// which reaches past its cell for a long name.
func (v *view) cellExtent(cl *cell) platform.Box {
	m, r := cl.m, cl.rect
	full := v.text(cl.item.Name, m.labelW, m.font, m.line, 8, false)
	if full == nil {
		return r
	}
	labelY := r.Y + m.iconTop + m.icon + m.labelGap
	r.Height = math.Max(r.Height, labelY+full.Height+m.pad/2-r.Y)
	return r
}

func boxID(cl *cell) string {
	if cl.box == nil {
		return ""
	}
	return cl.box.box.ID
}

// drawCell draws an item, with its whole name when expanded.
func (v *view) drawCell(c *platform.Canvas, cl *cell, expanded bool) {
	m, r := cl.m, cl.rect
	selected := v.selected[cl.item.Key] && v.selBox == boxID(cl)
	hovered := v.hover.cell != nil && v.hover.cell.item.Key == cl.item.Key && boxID(v.hover.cell) == boxID(cl)
	// An item a drag would drop onto shows as hovered.
	hovered = hovered || v.mark != nil && v.mark.onto == cl.item.Key && v.mark.box == boxID(cl)
	iconY := r.Y + m.iconTop
	labelY := iconY + m.icon + m.labelGap
	lines := 2
	if expanded {
		lines = 8
	}
	label := v.text(cl.item.Name, m.labelW, m.font, m.line, lines, false)
	labelH := m.line
	if label != nil {
		labelH = label.Height
	}
	hl := platform.Box{X: r.X + 1, Y: r.Y + 1, Width: r.Width - 2, Height: labelY + labelH + m.pad/2 - r.Y - 1}
	radius := math.Max(2, m.radius/2)
	switch {
	case selected:
		c.FillRoundRect(hl, radius, design.Dark.Primary.WithAlpha(0.32))
		c.StrokeRoundRect(platform.Box{X: hl.X + 0.5, Y: hl.Y + 0.5, Width: hl.Width - 1, Height: hl.Height - 1}, radius, 1, design.Dark.Primary.WithAlpha(0.75))
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
}

// drawBox draws a box and the items in it but skip, which Paint draws
// expanded over everything.
func (v *view) drawBox(c *platform.Canvas, g *boxGeo, dirty platform.Box, skip *cell) {
	m, b := g.m, g.box
	bg := boxColors[b.Color]
	bg.A = float64(v.settings.Opacity) / 100
	c.FillRoundRect(g.rect, m.radius, bg)
	border, width := white.WithAlpha(0.1), 1.0
	switch {
	case v.dropBox == b.ID:
		border, width = design.Dark.Primary.WithAlpha(0.9), math.Round(2*m.scale)
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
		return
	}
	c.FillRect(platform.Box{X: g.rect.X + m.pad, Y: g.title.Y + m.title - 1, Width: g.rect.Width - 2*m.pad, Height: 1}, white.WithAlpha(0.08))
	c.PushClip(g.content)
	for _, cl := range g.cells {
		if cl != skip && intersects(cl.rect, g.content) && intersects(cl.rect, dirty) {
			v.drawCell(c, cl, false)
		}
	}
	if mk := v.mark; mk != nil && mk.box == b.ID && mk.onto == "" && mk.rect.Width > 0 {
		c.FillRoundRect(mk.rect, mk.rect.Width/2, design.Dark.Primary)
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
}
