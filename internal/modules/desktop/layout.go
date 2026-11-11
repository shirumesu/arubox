package desktop

import "math"

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
