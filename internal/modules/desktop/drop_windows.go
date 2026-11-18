package desktop

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/shirumesu/arubox/internal/platform"
)

// springDelay is how long a drag rests over a rolled-up box before the box
// opens, so the drop can go to a place among its items. It is about what
// macOS waits for spring-loaded folders, a little over Windows' 400 ms
// hover time.
const springDelay = 500 * time.Millisecond

// dropMark shows where a drag would drop: onto the item onto, or, when
// that is "", between a box's items, before the item before, or at the end
// when that is "".
type dropMark struct {
	box    string
	onto   string
	before string
	rect   platform.Box // the item dropped onto, or the bar between items
}

// springWait is a drag resting over a rolled-up box.
type springWait struct{ box string }

// DropAt says what a drag over a point drops onto.
func (v *view) DropAt(x, y float64, internal bool) platform.DropTarget {
	v.lastX, v.lastY = x, y
	if v.hidden {
		return platform.DropTarget{}
	}
	h := v.hitTest(x, y)
	v.springOver(h)
	// dragged reports whether a cell is one of the items being dragged.
	dragged := func(c *cell) bool { return internal && v.selBox == boxID(c) && v.selected[c.item.Key] }
	if g := h.box; g != nil && g.box.Folder == "" {
		v.setDropBox(g.box.ID)
		onto, mark := v.spotAt(g, x, y, internal && v.selBox == g.box.ID)
		if onto != nil && takesDrops(onto.item) && !dragged(onto) {
			v.setMark(&dropMark{box: g.box.ID, onto: onto.item.Key, rect: onto.rect})
			return platform.DropTarget{Kind: platform.DropItem, Item: onto.item}
		}
		v.setMark(mark)
	} else {
		v.setDropBox("")
		if h.part == hitItem && takesDrops(h.cell.item) && !dragged(h.cell) {
			v.setMark(&dropMark{box: h.boxID(), onto: h.cell.item.Key, rect: h.cell.rect})
			return platform.DropTarget{Kind: platform.DropItem, Item: h.cell.item}
		}
		v.setMark(nil)
	}
	if p := v.portals[h.boxID()]; p != nil {
		return platform.DropTarget{Kind: platform.DropFolder, Path: p.path}
	}
	if internal && v.portals[v.selBox] == nil {
		return platform.DropTarget{Kind: platform.DropHere}
	}
	return platform.DropTarget{Kind: platform.DropFolder, Path: platform.DesktopFolder()}
}

// takesDrops reports whether things dropped onto an item go into it or to
// it: folders, programs, shortcuts and the Recycle Bin.
func takesDrops(it platform.ShellItem) bool {
	switch {
	case it.Folder, strings.HasPrefix(it.Key, "::"):
		return true
	}
	switch it.Ext {
	case ".exe", ".lnk", ".bat", ".cmd":
		return true
	}
	return false
}

// spotAt finds where a drag at a point over a box's open content would
// go: onto the item whose middle it is over, and else between the items,
// before the one whose nearer half it is over. own says the dragged items
// are the box's own. The mark is nil where the box keeps no place for the
// items: outside its open content, in a portal, and in a sorted box for
// items from elsewhere, which take their place from the sort.
func (v *view) spotAt(g *boxGeo, x, y float64, own bool) (*cell, *dropMark) {
	if g.box.Folder != "" || g.rect.Height <= g.m.title+1 || !contains(g.content, x, y) {
		return nil, nil
	}
	var onto *cell
	at := len(g.cells)
	for i, c := range g.cells {
		r := c.rect
		if contains(r, x, y) {
			if q := r.Width / 4; x >= r.X+q && x < r.X+r.Width-q {
				onto = c
			}
			at = i
			if x >= r.X+r.Width/2 {
				at = i + 1
			}
			break
		}
		if y < r.Y || (y < r.Y+r.Height && x < r.X+r.Width/2) {
			at = i
			break
		}
	}
	if g.box.Sort != "" && !own {
		return onto, nil
	}
	m := &dropMark{box: g.box.ID, rect: v.slotBar(g, at, y)}
	if at < len(g.cells) {
		m.before = g.cells[at].item.Key
	}
	return onto, m
}

// slotBar is the bar drawn where items go before the at-th cell of a box:
// after the end of the row the pointer is on, or before the cell.
func (v *view) slotBar(g *boxGeo, at int, y float64) platform.Box {
	cells, m := g.cells, g.m
	if len(cells) == 0 {
		return platform.Box{}
	}
	var r platform.Box
	var x float64
	if at > 0 && (at == len(cells) || y < cells[at].rect.Y) {
		r = cells[at-1].rect
		x = r.X + r.Width
	} else {
		r = cells[at].rect
		x = r.X
	}
	w := math.Max(2, math.Round(2*m.scale))
	x = math.Max(g.content.X, math.Min(x-w/2, g.content.X+g.content.Width-w))
	return platform.Box{X: x, Y: r.Y + m.pad/2, Width: w, Height: r.Height - m.pad}
}

func (v *view) setMark(m *dropMark) {
	old := v.mark
	if old == nil && m == nil || old != nil && m != nil && *old == *m {
		return
	}
	if old != nil {
		v.invalidate(old.rect)
	}
	v.mark = m
	if m != nil {
		v.invalidate(m.rect)
	}
}

// springOver follows a drag from box to box: one resting over a rolled-up
// box opens it after springDelay, and one leaving a box it opened rolls it
// up again.
func (v *view) springOver(h hit) {
	id := h.boxID()
	if v.sprung != "" && v.sprung != id {
		v.unspring()
	}
	if v.spring != nil && v.spring.box == id {
		return
	}
	v.spring = nil
	g := h.box
	if g == nil || !g.box.Rolled || v.peeking[id] {
		return
	}
	w := &springWait{box: id}
	v.spring = w
	time.AfterFunc(springDelay, func() {
		v.d.Do(func() {
			if v.stopping || v.spring != w {
				return
			}
			v.spring = nil
			g := v.geoOf(w.box)
			if g == nil || !g.box.Rolled || v.peeking[w.box] {
				return
			}
			v.peeking[w.box] = true
			v.sprung = w.box
			v.animateHeight(g, g.full.Height)
		})
	})
}

// unspring rolls up the box a drag opened.
func (v *view) unspring() {
	id := v.sprung
	v.sprung = ""
	if !v.peeking[id] {
		return
	}
	delete(v.peeking, id)
	if g := v.geoOf(id); g != nil && g.box.Rolled {
		v.animateHeight(g, g.m.title)
	}
}

// endDrag forgets what a drag showed. A box the drag opened stays open
// after a drop in it, as for a peek, until the pointer leaves it.
func (v *view) endDrag() {
	v.spring = nil
	v.sprung = ""
	v.setDropBox("")
	v.setMark(nil)
}

// DropLeft follows a drag leaving the layer, or given up, without a drop.
func (v *view) DropLeft() {
	v.spring = nil
	v.unspring()
	v.endDrag()
}

// Dropped finishes a drop: the layer's own items move, and files dropped
// in are placed once they appear.
func (v *view) Dropped(x, y float64, t platform.DropTarget) {
	mark := v.mark
	v.endDrag()
	h := v.hitTest(x, y)
	switch {
	case t.Kind == platform.DropHere:
		keys := v.selectedKeys()
		if g := h.box; g != nil && g.box.Folder == "" {
			v.putAt(g.box, keys, mark)
			v.selBox = g.box.ID
			v.arrange()
		} else {
			v.selBox = ""
			v.placeLoose(keys, x, y)
		}
		v.invalidateAll()
		v.save()
	case t.Kind == platform.DropFolder && t.Path == platform.DesktopFolder():
		v.pending = &placement{box: h.boxID(), x: x, y: y, mark: mark, until: time.Now().Add(10 * time.Second)}
	}
}

// putAt moves items into a box where a mark between its items shows, or
// to its end without one. A sorted box takes the order it shows as the
// user's own first, so the place chosen sticks.
func (v *view) putAt(b *Box, keys []string, mark *dropMark) {
	if mark == nil || mark.box != b.ID || mark.onto != "" {
		v.layout.put(b, keys, -1)
		return
	}
	if b.Sort != "" {
		var order []string
		for _, it := range v.boxItems(b) {
			order = append(order, it.Key)
		}
		for _, k := range b.Items {
			if !slices.Contains(order, k) {
				order = append(order, k)
			}
		}
		b.Items, b.Sort = order, ""
	}
	v.layout.put(b, keys, slices.Index(b.Items, mark.before))
}

// markAt is where items pasted or made at a point over a box go.
func (v *view) markAt(boxID string, x, y float64) *dropMark {
	g := v.geoOf(boxID)
	if g == nil {
		return nil
	}
	_, m := v.spotAt(g, x, y, false)
	return m
}
