package desktop

import (
	"maps"
	"math"
	"slices"
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
// keeps one layout on its own screen and another when docked. A box or
// icon an arrangement has no place for gets one worked out from the
// arrangement whose monitors are most like these.
type Arrangement struct {
	Boxes map[string]Place `json:"boxes"`
	Icons map[string]Cell  `json:"icons"`
	// Monitors are the monitors as last seen, so places can be carried
	// over to other monitors. Arrangements saved by older versions lack
	// them until their monitors are seen again.
	Monitors []Monitor `json:"monitors,omitempty"`
	// Used is when this set of monitors was last seen, in Unix seconds.
	Used int64 `json:"used"`
	// Edited is when the user last moved something here, in Unix seconds.
	Edited int64 `json:"edited,omitempty"`
}

// Layout is everything the module keeps about the desktop.
type Layout struct {
	Boxes        []Box                   `json:"boxes"`
	Arrangements map[string]*Arrangement `json:"arrangements"`
	// Files are the desktop's files' IDs by parsing name, as last seen,
	// so a file renamed while no one was watching, by another program
	// or while arubox was not running, keeps its place.
	Files map[string]string `json:"files,omitempty"`
}

// clone returns a copy sharing nothing with l, so the view can change
// its layout while the module saves another.
func (l Layout) clone() Layout {
	c := Layout{Boxes: slices.Clone(l.Boxes), Files: maps.Clone(l.Files)}
	for i := range c.Boxes {
		c.Boxes[i].Items = slices.Clone(c.Boxes[i].Items)
	}
	if l.Arrangements != nil {
		c.Arrangements = make(map[string]*Arrangement, len(l.Arrangements))
		for k, a := range l.Arrangements {
			n := *a
			n.Boxes, n.Icons, n.Monitors = maps.Clone(a.Boxes), maps.Clone(a.Icons), slices.Clone(a.Monitors)
			c.Arrangements[k] = &n
		}
	}
	return c
}

// Monitor is a display, in physical pixels of the virtual screen.
type Monitor struct {
	ID     string  `json:"id"`
	Bounds Rect    `json:"bounds"`
	Work   Rect    `json:"work"` // the bounds without the taskbar
	Scale  float64 `json:"scale"`
}

// workDIP is the monitor's work area in DIPs from its top left corner,
// the space boxes are carried over in.
func (m Monitor) workDIP() Rect {
	return Rect{(m.Work.X - m.Bounds.X) / m.Scale, (m.Work.Y - m.Bounds.Y) / m.Scale, m.Work.W / m.Scale, m.Work.H / m.Scale}
}
