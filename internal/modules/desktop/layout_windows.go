//go:build windows

package desktop

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/shirumesu/arubox/internal/platform"
)

// span makes the rectangle between two corners.
func span(x0, y0, x1, y1 float64) Rect {
	return Rect{math.Min(x0, x1), math.Min(y0, y1), math.Abs(x1 - x0), math.Abs(y1 - y0)}
}

// signature names a set of monitors.
func signature(monitors []Monitor) string {
	parts := make([]string, len(monitors))
	for i, m := range monitors {
		parts[i] = fmt.Sprintf("%s:%gx%g@%g", m.ID, m.Bounds.W, m.Bounds.H, m.Scale)
	}
	slices.Sort(parts)
	return strings.Join(parts, "|")
}

// arrangement returns the arrangement for a set of monitors, a new empty
// one for a set seen for the first time.
func (l *Layout) arrangement(monitors []Monitor) *Arrangement {
	if l.Arrangements == nil {
		l.Arrangements = map[string]*Arrangement{}
	}
	sig := signature(monitors)
	a := l.Arrangements[sig]
	if a == nil {
		a = &Arrangement{Boxes: map[string]Place{}, Icons: map[string]Cell{}}
		l.Arrangements[sig] = a
	}
	a.Monitors = slices.Clone(monitors)
	a.Used = time.Now().Unix()
	return a
}

// source returns the arrangement other than cur, with monitors known and
// passing has, whose places carry over to cur's monitors best: the one
// whose primary monitor's work area is nearest in size, then the one
// edited, then used, last. It returns nil if there is none.
func (l *Layout) source(cur *Arrangement, has func(*Arrangement) bool) *Arrangement {
	var best *Arrangement
	bestD := math.Inf(1)
	for _, a := range l.Arrangements {
		if a == cur || len(a.Monitors) == 0 || !has(a) {
			continue
		}
		d := unlike(a.Monitors, cur.Monitors)
		if best == nil || d < bestD-1e-9 || (math.Abs(d-bestD) <= 1e-9 && (a.Edited > best.Edited || (a.Edited == best.Edited && a.Used > best.Used))) {
			best, bestD = a, d
		}
	}
	return best
}

// unlike measures how far apart two sets of monitors are: by how much
// their primary monitors' work areas differ in size, much further for
// another number of monitors.
func unlike(a, b []Monitor) float64 {
	wa, wb := a[0].workDIP(), b[0].workDIP()
	d := math.Abs(math.Log(wa.W/wb.W)) + math.Abs(math.Log(wa.H/wb.H))
	if len(a) != len(b) {
		d += 10
	}
	return d
}

// counterpart returns the monitor of to that stands for the monitor id of
// from: the one with the same ID, else the one at the same place among
// the monitors that are in only one of the sets, else the primary one.
func counterpart(id string, from, to []Monitor) Monitor {
	in := func(ms []Monitor, id string) bool {
		return slices.ContainsFunc(ms, func(m Monitor) bool { return m.ID == id })
	}
	if i := slices.IndexFunc(to, func(m Monitor) bool { return m.ID == id }); i >= 0 {
		return to[i]
	}
	var gone, added []Monitor
	for _, m := range from {
		if !in(to, m.ID) {
			gone = append(gone, m)
		}
	}
	for _, m := range to {
		if !in(from, m.ID) {
			added = append(added, m)
		}
	}
	if i := slices.IndexFunc(gone, func(m Monitor) bool { return m.ID == id }); i >= 0 && i < len(added) {
		return added[i]
	}
	return to[0]
}

// carry maps a place on the monitors from to the monitors to. It returns
// false if from does not have the place's monitor.
//
// Each axis is mapped on its own, in the monitors' work areas. One that
// shrinks shrinks everything on it alike, sizes too, so boxes keep their
// look and never come to overlap. One that grows keeps the sizes, and
// keeps each box as far from the end it is nearer to as it was: boxes
// in the near half stay, those in the far half move along with the far
// edge, so those apart stay apart there too.
func carry(p Place, from, to []Monitor) (Place, bool) {
	i := slices.IndexFunc(from, func(m Monitor) bool { return m.ID == p.Monitor })
	if i < 0 {
		return Place{}, false
	}
	src, dst := from[i].workDIP(), counterpart(p.Monitor, from, to)
	dw := dst.workDIP()
	x, w := carrySpan(p.Rect.X-src.X, p.Rect.W, src.W, dw.W)
	y, h := carrySpan(p.Rect.Y-src.Y, p.Rect.H, src.H, dw.H)
	return Place{Monitor: dst.ID, Rect: Rect{dw.X + x, dw.Y + y, w, h}}, true
}

// carrySpan maps the span from a of length n on an axis of length from
// onto one of length to.
func carrySpan(a, n, from, to float64) (float64, float64) {
	if to < from {
		k := to / from
		return a * k, n * k
	}
	if a+n/2 > from/2 {
		return a + to - from, n
	}
	return a, n
}

// freeSpot returns the place nearest r, inside area, where r overlaps none
// of others and keeps gap from them. The places tried are those beside
// the area's edges and the others' edges; if r fits nowhere it is
// returned as it is.
func freeSpot(r, area Rect, others []Rect, gap float64) Rect {
	free := func(c Rect) bool {
		if c.X < area.X-0.5 || c.Y < area.Y-0.5 || c.Right() > area.Right()+0.5 || c.Bottom() > area.Bottom()+0.5 {
			return false
		}
		g := c.Inset(-gap + 0.5)
		return !slices.ContainsFunc(others, func(o Rect) bool {
			return g.X < o.Right() && o.X < g.Right() && g.Y < o.Bottom() && o.Y < g.Bottom()
		})
	}
	if free(r) {
		return r
	}
	xs := []float64{r.X, area.X, area.Right() - r.W}
	ys := []float64{r.Y, area.Y, area.Bottom() - r.H}
	for _, o := range others {
		xs = append(xs, o.Right()+gap, o.X-gap-r.W, o.X)
		ys = append(ys, o.Bottom()+gap, o.Y-gap-r.H, o.Y)
	}
	best, bestD := r, math.Inf(1)
	for _, x := range xs {
		for _, y := range ys {
			c := Rect{x, y, r.W, r.H}
			if d := (x-r.X)*(x-r.X) + (y-r.Y)*(y-r.Y); d < bestD && free(c) {
				best, bestD = c, d
			}
		}
	}
	return best
}

// boxOf returns the box holding a desktop item, or nil.
func (l *Layout) boxOf(key string) *Box {
	for i := range l.Boxes {
		if l.Boxes[i].Folder == "" && slices.Contains(l.Boxes[i].Items, key) {
			return &l.Boxes[i]
		}
	}
	return nil
}

func (l *Layout) box(id string) *Box {
	for i := range l.Boxes {
		if l.Boxes[i].ID == id {
			return &l.Boxes[i]
		}
	}
	return nil
}

// take removes items from whichever box holds them.
func (l *Layout) take(keys []string) {
	for i := range l.Boxes {
		l.Boxes[i].Items = slices.DeleteFunc(l.Boxes[i].Items, func(k string) bool { return slices.Contains(keys, k) })
	}
}

// put moves items into a box before position at, or to its end when at is
// out of range.
func (l *Layout) put(b *Box, keys []string, at int) {
	// Items moved within the box from before the position shift it back,
	// each by one, as they stood before any is taken out.
	shift := 0
	for _, k := range keys {
		if i := slices.Index(b.Items, k); i >= 0 && i < at {
			shift++
		}
	}
	at -= shift
	l.take(keys)
	if at < 0 || at > len(b.Items) {
		at = len(b.Items)
	}
	b.Items = slices.Insert(b.Items, at, keys...)
}

// rename follows an item to its new name.
func (l *Layout) rename(old, now string) {
	for i := range l.Boxes {
		if j := slices.Index(l.Boxes[i].Items, old); j >= 0 {
			l.Boxes[i].Items[j] = now
		}
	}
	for _, a := range l.Arrangements {
		if c, ok := a.Icons[old]; ok {
			delete(a.Icons, old)
			a.Icons[now] = c
		}
	}
}

// placed reports whether an item has a place: in a box, or on the grid
// of any set of monitors.
func (l *Layout) placed(key string) bool {
	if l.boxOf(key) != nil {
		return true
	}
	for _, a := range l.Arrangements {
		if _, ok := a.Icons[key]; ok {
			return true
		}
	}
	return false
}

// follow moves the places of items gone from the desktop, or whose names
// another item now has, to the items now there with the same file ID,
// unless those have places of their own, and
// then records the IDs of the items there are. It returns the renames, old
// names first.
func (l *Layout) follow(items map[string]platform.ShellItem) [][2]string {
	byID := map[string]string{}
	for k, it := range items {
		if it.ID != "" {
			byID[it.ID] = k
		}
	}
	var renames [][2]string
	for old, id := range l.Files {
		// An item there under the old name with another ID is a new one
		// that took the name of one renamed.
		if it, ok := items[old]; ok && it.ID == id {
			continue
		}
		if now, ok := byID[id]; ok && !l.placed(now) {
			l.rename(old, now)
			renames = append(renames, [2]string{old, now})
		}
	}
	l.Files = make(map[string]string, len(byID))
	for id, k := range byID {
		l.Files[k] = id
	}
	return renames
}

// forget drops items that no longer exist.
func (l *Layout) forget(exists func(string) bool) {
	for i := range l.Boxes {
		l.Boxes[i].Items = slices.DeleteFunc(l.Boxes[i].Items, func(k string) bool { return !exists(k) })
	}
	for _, a := range l.Arrangements {
		maps.DeleteFunc(a.Icons, func(k string, _ Cell) bool { return !exists(k) })
	}
}

// newBoxID returns an ID no box has.
func (l *Layout) newBoxID() string {
	for n := len(l.Boxes) + 1; ; n++ {
		id := fmt.Sprintf("box%d", n)
		if l.box(id) == nil {
			return id
		}
	}
}

// Item is what sorting needs of a desktop item.
type sortable struct {
	key, name, ext string
	folder         bool
	modified       time.Time
}

// sortItems orders a box's items by its sort mode; folders come first in
// every mode but the user's own order.
func sortItems(items []sortable, mode string) {
	if mode == "" {
		return
	}
	slices.SortStableFunc(items, func(a, b sortable) int {
		if a.folder != b.folder {
			if a.folder {
				return -1
			}
			return 1
		}
		switch mode {
		case "type":
			if c := cmp.Compare(a.ext, b.ext); c != 0 {
				return c
			}
		case "date":
			if c := b.modified.Compare(a.modified); c != 0 {
				return c
			}
		}
		return cmp.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
	})
}

// A box snaps to another only where they are neighbours, so boxes far away
// do not pull at it, and each side of another box offers one place, so an
// edge does not flip between touching it and keeping the gap. Across, r
// goes beside a box level with it, or lines its left or right edge up with
// a box just above or below it; down likewise. What is level or just above
// is judged from r as the pointer puts it, so the same pointer position
// always snaps the same way.

// overlaps reports whether the span [a0, a1) overlaps [b0, b1) by more
// than d, so a box pushed up to d into another, as snapping beside it may
// undo, still counts as beside it.
func overlaps(a0, a1, b0, b1, d float64) bool { return a0+d < b1 && b0+d < a1 }

// near reports whether the spans are apart by at most d.
func near(a0, a1, b0, b1, d float64) bool { return a0 <= b1+d && b0 <= a1+d }

// snapMove moves r so its edges meet nearby edges of the area or of
// neighbouring boxes, keeping a gap beside them, when they are within
// reach. It reports whether it moved r across and down.
func snapMove(r Rect, area Rect, others []Rect, reach, gap float64) (Rect, bool, bool) {
	xs := []float64{area.X, area.Right() - r.W}
	ys := []float64{area.Y, area.Bottom() - r.H}
	for _, o := range others {
		level := overlaps(r.Y, r.Bottom(), o.Y, o.Bottom(), reach)
		column := overlaps(r.X, r.Right(), o.X, o.Right(), reach)
		switch {
		case level:
			xs = append(xs, o.Right()+gap, o.X-gap-r.W)
		case near(r.Y, r.Bottom(), o.Y, o.Bottom(), gap+reach):
			xs = append(xs, o.X, o.Right()-r.W)
		}
		switch {
		case column:
			ys = append(ys, o.Bottom()+gap, o.Y-gap-r.H)
		case near(r.X, r.Right(), o.X, o.Right(), gap+reach):
			ys = append(ys, o.Y, o.Bottom()-r.H)
		}
	}
	var sx, sy bool
	r.X, sx = nearest(r.X, xs, reach)
	r.Y, sy = nearest(r.Y, ys, reach)
	return r, sx, sy
}

// snapEdge moves the position v of one edge of r, a left or right one when
// vertical, to a nearby edge of the area or of a neighbouring box.
func snapEdge(v float64, r Rect, area Rect, others []Rect, vertical bool, reach, gap float64) float64 {
	var c []float64
	if vertical {
		c = append(c, area.X, area.Right())
		for _, o := range others {
			switch {
			case overlaps(r.Y, r.Bottom(), o.Y, o.Bottom(), reach):
				c = append(c, o.X-gap, o.Right()+gap)
			case near(r.Y, r.Bottom(), o.Y, o.Bottom(), gap+reach):
				c = append(c, o.X, o.Right())
			}
		}
	} else {
		c = append(c, area.Y, area.Bottom())
		for _, o := range others {
			switch {
			case overlaps(r.X, r.Right(), o.X, o.Right(), reach):
				c = append(c, o.Y-gap, o.Bottom()+gap)
			case near(r.X, r.Right(), o.X, o.Right(), gap+reach):
				c = append(c, o.Y, o.Bottom())
			}
		}
	}
	v, _ = nearest(v, c, reach)
	return v
}

// nearest returns the candidate nearest to v within reach, and whether
// there was one; else v.
func nearest(v float64, candidates []float64, reach float64) (float64, bool) {
	best, dist, found := v, reach, false
	for _, c := range candidates {
		if d := math.Abs(c - v); d < dist {
			best, dist, found = c, d, true
		}
	}
	return best, found
}

// toGrid moves v to the nearest line of a grid of steps from origin.
func toGrid(v, origin, step float64) float64 {
	return origin + math.Round((v-origin)/step)*step
}

// toCells rounds a box's length to a whole number of cells, at least one,
// plus the frame around them.
func toCells(length, step, frame float64) float64 {
	return frame + max(1, math.Round((length-frame)/step))*step
}
