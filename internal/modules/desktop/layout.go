package desktop

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
)

// Rect is a rectangle; its unit depends on where it is used.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Right is the rectangle's right edge.
func (r Rect) Right() float64 { return r.X + r.W }

// Bottom is the rectangle's bottom edge.
func (r Rect) Bottom() float64 { return r.Y + r.H }

// Inset shrinks the rectangle by d on every side.
func (r Rect) Inset(d float64) Rect { return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }

// Union returns the smallest rectangle holding both; an empty one is left
// out.
func (r Rect) Union(o Rect) Rect {
	if r.W <= 0 || r.H <= 0 {
		return o
	}
	if o.W <= 0 || o.H <= 0 {
		return r
	}
	x, y := math.Min(r.X, o.X), math.Min(r.Y, o.Y)
	return Rect{x, y, math.Max(r.Right(), o.Right()) - x, math.Max(r.Bottom(), o.Bottom()) - y}
}

// span makes the rectangle between two corners.
func span(x0, y0, x1, y1 float64) Rect {
	return Rect{math.Min(x0, x1), math.Min(y0, y1), math.Abs(x1 - x0), math.Abs(y1 - y0)}
}

// Box is a group of desktop items, or a folder portal that shows a folder.
type Box struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Folder makes the box a folder portal showing this folder.
	Folder string `json:"folder,omitempty"`
	// Items are the desktop items in a box, by parsing name, in the order
	// the user arranged them.
	Items []string `json:"items,omitempty"`
	// Sort orders the items: "" as arranged, or "name", "type" or "date".
	Sort string `json:"sort,omitempty"`
	// Rolled shows only the title bar.
	Rolled bool `json:"rolled,omitempty"`
	// Color is the name of one of the box colours, or "" for the default.
	Color string `json:"color,omitempty"`
}

// Place is where a box is on one monitor, in DIPs from the monitor's top
// left corner.
type Place struct {
	Monitor string `json:"monitor"`
	Rect    Rect   `json:"rect"`
}

// Cell is where a loose icon is on a monitor's grid.
type Cell struct {
	Monitor string `json:"monitor"`
	Col     int    `json:"col"`
	Row     int    `json:"row"`
}

// Arrangement is where everything is for one set of monitors, so a laptop
// keeps one layout on its own screen and another when docked.
type Arrangement struct {
	Boxes map[string]Place `json:"boxes"`
	Icons map[string]Cell  `json:"icons"`
	// Used is when this set of monitors was last seen, in Unix seconds.
	Used int64 `json:"used"`
}

// Layout is everything the module keeps about the desktop.
type Layout struct {
	Boxes        []Box                   `json:"boxes"`
	Arrangements map[string]*Arrangement `json:"arrangements"`
}

// Monitor is a display, in physical pixels of the virtual screen.
type Monitor struct {
	ID     string
	Bounds Rect
	Work   Rect // the bounds without the taskbar
	Scale  float64
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

// arrangement returns the arrangement for a set of monitors. A set seen for
// the first time starts from the one used last, with boxes of missing
// monitors moved to the first monitor and every box kept on its monitor.
func (l *Layout) arrangement(monitors []Monitor) *Arrangement {
	if l.Arrangements == nil {
		l.Arrangements = map[string]*Arrangement{}
	}
	sig := signature(monitors)
	a := l.Arrangements[sig]
	if a == nil {
		a = &Arrangement{Boxes: map[string]Place{}, Icons: map[string]Cell{}}
		var last *Arrangement
		for _, o := range l.Arrangements {
			if last == nil || o.Used > last.Used {
				last = o
			}
		}
		if last != nil {
			byID := map[string]Monitor{}
			for _, m := range monitors {
				byID[m.ID] = m
			}
			for id, p := range last.Boxes {
				m, ok := byID[p.Monitor]
				if !ok {
					m = monitors[0]
				}
				size := Rect{W: m.Bounds.W / m.Scale, H: m.Bounds.H / m.Scale}
				p.Monitor = m.ID
				p.Rect.W, p.Rect.H = math.Min(p.Rect.W, size.W), math.Min(p.Rect.H, size.H)
				p.Rect.X = math.Max(0, math.Min(p.Rect.X, size.W-p.Rect.W))
				p.Rect.Y = math.Max(0, math.Min(p.Rect.Y, size.H-p.Rect.H))
				a.Boxes[id] = p
			}
		}
		l.Arrangements[sig] = a
	}
	a.Used = time.Now().Unix()
	return a
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
	// Items moved within the box shift the position they go to.
	for _, k := range keys {
		if i := slices.Index(b.Items, k); i >= 0 && i < at {
			at--
		}
	}
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

// snapMove moves r so its edges meet nearby edges of the area or of other
// boxes, keeping a gap between boxes, when they are within reach.
func snapMove(r Rect, area Rect, others []Rect, reach, gap float64) Rect {
	xs := []float64{area.X, area.Right() - r.W}
	ys := []float64{area.Y, area.Bottom() - r.H}
	for _, o := range others {
		// Beside the other box, or lined up with it.
		xs = append(xs, o.Right()+gap, o.X-gap-r.W, o.X, o.Right()-r.W)
		ys = append(ys, o.Bottom()+gap, o.Y-gap-r.H, o.Y, o.Bottom()-r.H)
	}
	r.X = nearest(r.X, xs, reach)
	r.Y = nearest(r.Y, ys, reach)
	return r
}

// snapEdge moves one edge's position v to a nearby edge.
func snapEdge(v float64, area Rect, others []Rect, vertical bool, reach, gap float64) float64 {
	var c []float64
	if vertical {
		c = append(c, area.X, area.Right())
		for _, o := range others {
			c = append(c, o.X, o.Right(), o.X-gap, o.Right()+gap)
		}
	} else {
		c = append(c, area.Y, area.Bottom())
		for _, o := range others {
			c = append(c, o.Y, o.Bottom(), o.Y-gap, o.Bottom()+gap)
		}
	}
	return nearest(v, c, reach)
}

func nearest(v float64, candidates []float64, reach float64) float64 {
	best, dist := v, reach
	for _, c := range candidates {
		if d := math.Abs(c - v); d < dist {
			best, dist = c, d
		}
	}
	return best
}
