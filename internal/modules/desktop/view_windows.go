package desktop

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/platform"
)

// The view is the desktop as the module draws it. Everything in it runs on
// the desktop layer's UI thread, except stop and setSettings, which hand
// their work to it.

// metrics are the sizes of things on one monitor, in its pixels.
type metrics struct {
	scale                         float64
	icon, cellW, cellH            float64
	iconTop, labelGap, labelW     float64
	font, line, titleFont, title  float64
	pad, margin, radius, edge     float64
	reach, gap, drag, scrollWidth float64
}

func (v *view) metrics(scale float64) metrics {
	s := func(dip float64) float64 { return math.Round(dip * scale) }
	icon := float64(v.settings.IconSize)
	return metrics{
		scale:       scale,
		icon:        s(icon),
		cellW:       s(icon + 32),
		cellH:       s(icon + 46),
		iconTop:     s(6),
		labelGap:    s(4),
		labelW:      s(icon + 26),
		font:        12 * scale,
		line:        s(16),
		titleFont:   13 * scale,
		title:       s(32),
		pad:         s(6),
		margin:      s(10),
		radius:      v.tokens.Radius * scale,
		edge:        s(6),
		reach:       s(12),
		gap:         s(8),
		drag:        s(5),
		scrollWidth: s(4),
	}
}

// cell is an item laid out on the desktop or in a box.
type cell struct {
	item platform.ShellItem
	rect platform.Box
	m    metrics
	box  *boxGeo // nil for a loose icon
}

// boxGeo is a box laid out on its monitor.
type boxGeo struct {
	box      *Box
	mon      platform.DesktopMonitor
	m        metrics
	rect     platform.Box // as drawn, at its current height
	full     platform.Box // unrolled
	title    platform.Box
	content  platform.Box
	cells    []*cell
	contentH float64
	scroll   float64
}

// portal is what a folder portal shows: the folder it is in now and the
// folders it came from.
type portal struct {
	path  string
	back  []string
	items []platform.ShellItem
}

type iconKey struct {
	key string
	px  int
}

type labelKey struct {
	text  string
	width int
	size  int
	lines int
	bold  bool
}

type view struct {
	m        *Module
	d        *platform.Desktop
	tokens   design.Tokens
	settings Settings
	layout   Layout
	monitors []platform.DesktopMonitor
	arr      *Arrangement

	items   map[string]platform.ShellItem // the desktop's items by key
	order   []string                      // in the order the shell lists them
	portals map[string]*portal            // by box ID

	boxes []*boxGeo // in drawing order
	loose []*cell

	pixels  map[iconKey]platform.Image
	bitmaps map[*platform.Canvas]map[iconKey]*platform.Bitmap
	loading map[iconKey]bool
	icons   chan iconKey
	texts   map[labelKey]*platform.TextLayout

	selected map[string]bool
	selBox   string // the box the selection is in, or "" for the desktop
	anchor   string // where a Shift selection starts
	hover    hit
	press    *press
	band     platform.Box
	hidden   bool
	heights  map[string]*anim // rolling boxes
	peeking  map[string]bool  // rolled-up boxes open while hovered
	scroll   map[string]float64
	editing  *editing
	pending  *placement
	dropBox  string
	lastX    float64 // where the pointer last was
	lastY    float64

	refreshQueued bool
	saveQueued    bool
	stopping      bool
}

// anim moves a box's height from one value to another.
type anim struct {
	from, to float64
	start    time.Time
}

const animTime = 160 * time.Millisecond

// placement is where items about to appear go: dropped or pasted onto a
// box or a place on the desktop.
type placement struct {
	box   string
	x, y  float64
	until time.Time
}

// editing is a name being edited in place.
type editing struct {
	key   string // an item's, or "" for a box title
	box   string
	known string
}

func startView(m *Module, s stored) (*view, error) {
	v := &view{
		m: m, tokens: m.tokens, settings: s.Settings, layout: s.Layout,
		items: map[string]platform.ShellItem{}, portals: map[string]*portal{},
		pixels: map[iconKey]platform.Image{}, bitmaps: map[*platform.Canvas]map[iconKey]*platform.Bitmap{},
		loading: map[iconKey]bool{}, icons: make(chan iconKey, 4096), texts: map[labelKey]*platform.TextLayout{},
		selected: map[string]bool{}, heights: map[string]*anim{}, peeking: map[string]bool{}, scroll: map[string]float64{},
	}
	go v.loadIcons()
	if _, err := platform.AttachDesktop(v); err != nil {
		close(v.icons)
		return nil, err
	}
	return v, nil
}

// stop saves the layout and gives the desktop back.
func (v *view) stop() {
	v.d.Do(func() {
		v.stopping = true
		v.m.saveLayout(v.layout)
	})
	v.d.Detach()
	close(v.icons)
}

func (v *view) setSettings(s Settings) {
	v.d.Do(func() {
		sizeChanged := s.IconSize != v.settings.IconSize
		v.settings = s
		if sizeChanged {
			v.dropIcons()
		}
		v.arrange()
		v.invalidateAll()
	})
}

// Attached is called on the UI thread before the monitors.
func (v *view) Attached(d *platform.Desktop) {
	v.d = d
	v.loadItems()
}

// Monitors lays everything out for a new set of monitors.
func (v *view) Monitors(monitors []platform.DesktopMonitor) {
	v.monitors = monitors
	v.arr = v.layout.arrangement(toMonitors(monitors))
	for _, byKey := range v.bitmaps {
		for _, b := range byKey {
			b.Release()
		}
	}
	clear(v.bitmaps)
	v.arrange()
	v.watch()
}

func toMonitors(ms []platform.DesktopMonitor) []Monitor {
	out := make([]Monitor, len(ms))
	for i, m := range ms {
		out[i] = Monitor{ID: m.ID, Bounds: fromBox(m.Bounds), Work: fromBox(m.Work), Scale: m.Scale}
	}
	return out
}

func fromBox(b platform.Box) Rect { return Rect{b.X, b.Y, b.Width, b.Height} }

func toBox(r Rect) platform.Box { return platform.Box{X: r.X, Y: r.Y, Width: r.W, Height: r.H} }

// loadItems reads the desktop's items and the portals' folders.
func (v *view) loadItems() {
	items, err := platform.DesktopItems()
	if err != nil {
		v.m.log.Warn("list desktop items", "error", err)
		return
	}
	known := v.items
	v.items = make(map[string]platform.ShellItem, len(items))
	v.order = v.order[:0]
	var added []string
	for _, it := range items {
		if old, ok := known[it.Key]; ok && !old.Modified.Equal(it.Modified) {
			v.forgetIcon(it.Key)
		}
		if _, ok := known[it.Key]; !ok && len(known) > 0 {
			added = append(added, it.Key)
		}
		v.items[it.Key] = it
		v.order = append(v.order, it.Key)
	}
	// The Recycle Bin's icon shows whether it is empty.
	for k := range v.items {
		if strings.HasPrefix(k, "::") {
			v.forgetIcon(k)
		}
	}
	v.layout.forget(func(k string) bool { _, ok := v.items[k]; return ok })
	for i := range v.layout.Boxes {
		b := &v.layout.Boxes[i]
		if b.Folder == "" {
			continue
		}
		p := v.portals[b.ID]
		if p == nil {
			p = &portal{path: b.Folder}
			v.portals[b.ID] = p
		}
		v.loadPortal(p)
	}
	if len(added) > 0 {
		v.placeNew(added)
	}
	// Selected items that are gone are no longer selected.
	shown := map[string]bool{}
	if p := v.portals[v.selBox]; p != nil {
		for _, it := range p.items {
			shown[it.Key] = true
		}
	} else {
		for k := range v.items {
			shown[k] = true
		}
	}
	for k := range v.selected {
		if !shown[k] {
			delete(v.selected, k)
		}
	}
}

func (v *view) loadPortal(p *portal) {
	items, err := platform.FolderItems(p.path)
	if err != nil {
		p.items = nil
		return
	}
	for _, it := range items {
		for _, old := range p.items {
			if old.Key == it.Key && !old.Modified.Equal(it.Modified) {
				v.forgetIcon(it.Key)
			}
		}
	}
	p.items = items
}

// placeNew puts items that just appeared where they were dropped or
// pasted, or else in the first free place on the desktop.
func (v *view) placeNew(keys []string) {
	if p := v.pending; p != nil && time.Now().Before(p.until) {
		if b := v.layout.box(p.box); b != nil && b.Folder == "" {
			v.layout.put(b, keys, v.insertIndex(v.geoOf(b.ID), p.x, p.y))
			v.save()
			return
		}
		if p.box == "" {
			v.arrange()
			v.placeLoose(keys, p.x, p.y)
			v.save()
			return
		}
	}
	v.save()
}

// watch follows the desktop and the portals' folders.
func (v *view) watch() {
	var folders []string
	for _, p := range v.portals {
		folders = append(folders, p.path)
	}
	slices.Sort(folders)
	v.d.Watch(folders)
}

// ShellChanged follows renames at once and reads the items again shortly,
// once a burst of changes is over.
func (v *view) ShellChanged(renames [][2]string) {
	for _, r := range renames {
		if r[0] != "" && r[1] != "" {
			v.layout.rename(r[0], r[1])
			if v.selected[r[0]] {
				delete(v.selected, r[0])
				v.selected[r[1]] = true
			}
		}
	}
	if v.refreshQueued {
		return
	}
	v.refreshQueued = true
	time.AfterFunc(150*time.Millisecond, func() {
		v.d.Do(func() {
			v.refreshQueued = false
			if v.stopping {
				return
			}
			v.loadItems()
			v.arrange()
			v.invalidateAll()
		})
	})
}

// save keeps the layout shortly, once a burst of changes is over.
func (v *view) save() {
	if v.saveQueued {
		return
	}
	v.saveQueued = true
	time.AfterFunc(500*time.Millisecond, func() {
		v.d.Do(func() {
			v.saveQueued = false
			if !v.stopping {
				v.m.saveLayout(v.layout)
			}
		})
	})
}

// monitor returns the monitor with an ID, or the first.
func (v *view) monitor(id string) platform.DesktopMonitor {
	for _, m := range v.monitors {
		if m.ID == id {
			return m
		}
	}
	return v.monitors[0]
}

// monitorAt returns the monitor holding a point, or the nearest.
func (v *view) monitorAt(x, y float64) platform.DesktopMonitor {
	best, dist := v.monitors[0], math.Inf(1)
	for _, m := range v.monitors {
		b := m.Bounds
		dx := math.Max(0, math.Max(b.X-x, x-(b.X+b.Width)))
		dy := math.Max(0, math.Max(b.Y-y, y-(b.Y+b.Height)))
		if d := dx*dx + dy*dy; d < dist {
			best, dist = m, d
		}
	}
	return best
}

// placeRect turns a box's place into screen pixels.
func placeRect(p Place, mon platform.DesktopMonitor) platform.Box {
	s := mon.Scale
	return platform.Box{X: mon.Bounds.X + p.Rect.X*s, Y: mon.Bounds.Y + p.Rect.Y*s, Width: p.Rect.W * s, Height: p.Rect.H * s}
}

// setPlace stores a box's rectangle, in screen pixels, on the monitor
// holding its centre.
func (v *view) setPlace(id string, r platform.Box) {
	mon := v.monitorAt(r.X+r.Width/2, r.Y+r.Height/2)
	s := mon.Scale
	v.arr.Boxes[id] = Place{Monitor: mon.ID, Rect: Rect{(r.X - mon.Bounds.X) / s, (r.Y - mon.Bounds.Y) / s, r.Width / s, r.Height / s}}
}

// arrange lays out the boxes and the loose icons.
func (v *view) arrange() {
	if len(v.monitors) == 0 {
		return
	}
	v.boxes = v.boxes[:0]
	for i := range v.layout.Boxes {
		b := &v.layout.Boxes[i]
		p, ok := v.arr.Boxes[b.ID]
		if !ok {
			// A box new to these monitors goes to the top right corner.
			mon := v.monitors[0]
			p = Place{Monitor: mon.ID, Rect: Rect{X: (mon.Work.X-mon.Bounds.X+mon.Work.Width)/mon.Scale - 320, Y: (mon.Work.Y-mon.Bounds.Y)/mon.Scale + 20, W: 300, H: 240}}
			v.arr.Boxes[b.ID] = p
		}
		mon := v.monitor(p.Monitor)
		g := &boxGeo{box: b, mon: mon, m: v.metrics(mon.Scale), full: placeRect(p, mon)}
		v.layoutBox(g)
		v.boxes = append(v.boxes, g)
	}
	v.arrangeLoose()
}

// layoutBox places a box's title, content and items.
func (v *view) layoutBox(g *boxGeo) {
	m := g.m
	g.title = platform.Box{X: g.full.X, Y: g.full.Y, Width: g.full.Width, Height: m.title}
	g.rect = g.full
	if a := v.heights[g.box.ID]; a != nil {
		g.rect.Height = v.animHeight(a)
	} else if g.box.Rolled && !v.peeking[g.box.ID] {
		g.rect.Height = m.title
	}
	g.content = platform.Box{X: g.full.X + m.pad, Y: g.full.Y + m.title, Width: g.full.Width - 2*m.pad, Height: g.rect.Height - m.title - m.pad}
	items := v.boxItems(g.box)
	cols := max(1, int(g.content.Width/m.cellW))
	cw := g.content.Width / float64(cols)
	rows := (len(items) + cols - 1) / cols
	g.contentH = float64(rows) * m.cellH
	visible := g.full.Height - m.title - m.pad
	g.scroll = math.Max(0, math.Min(v.scroll[g.box.ID]*m.scale, g.contentH-visible))
	g.cells = g.cells[:0]
	for i, it := range items {
		r := platform.Box{X: g.content.X + float64(i%cols)*cw, Y: g.content.Y + float64(i/cols)*m.cellH - g.scroll, Width: cw, Height: m.cellH}
		g.cells = append(g.cells, &cell{item: it, rect: r, m: m, box: g})
	}
}

// boxItems returns the items a box shows, in its order.
func (v *view) boxItems(b *Box) []platform.ShellItem {
	var items []platform.ShellItem
	if b.Folder != "" {
		if p := v.portals[b.ID]; p != nil {
			items = slices.Clone(p.items)
		}
	} else {
		for _, k := range b.Items {
			if it, ok := v.items[k]; ok {
				items = append(items, it)
			}
		}
	}
	mode := b.Sort
	if b.Folder != "" && mode == "" {
		mode = "name"
	}
	if mode == "" {
		return items
	}
	s := make([]sortable, len(items))
	byKey := make(map[string]platform.ShellItem, len(items))
	for i, it := range items {
		s[i] = sortable{key: it.Key, name: it.Name, ext: it.Ext, folder: it.Folder, modified: it.Modified}
		byKey[it.Key] = it
	}
	sortItems(s, mode)
	for i, x := range s {
		items[i] = byKey[x.key]
	}
	return items
}

// arrangeLoose places the icons not in a box on their monitors' grids,
// giving a free cell to icons without one.
func (v *view) arrangeLoose() {
	v.loose = v.loose[:0]
	taken := map[Cell]bool{}
	var unplaced []string
	for _, k := range v.order {
		if v.layout.boxOf(k) != nil {
			continue
		}
		c, ok := v.arr.Icons[k]
		if !ok || taken[c] || !v.cellFree(c, nil) {
			unplaced = append(unplaced, k)
			continue
		}
		taken[c] = true
		v.loose = append(v.loose, v.looseCell(k, c))
	}
	place := func(k string, c Cell) {
		taken[c] = true
		v.arr.Icons[k] = c
		v.loose = append(v.loose, v.looseCell(k, c))
	}
	// Icons new to the layout go where File Explorer had them, in its
	// column order, so a first takeover keeps the desktop as it was.
	if len(unplaced) > 0 {
		waiting := map[string]bool{}
		for _, k := range unplaced {
			waiting[k] = true
		}
		for _, e := range v.d.ExplorerIcons() {
			if !waiting[e.Key] {
				continue
			}
			x, y := e.Cell.X+e.Cell.Width/2, e.Cell.Y+e.Cell.Height/2
			if c, ok := v.freeCell(taken, v.monitorAt(x, y), x, y); ok {
				place(e.Key, c)
				delete(waiting, e.Key)
			}
		}
		for _, k := range unplaced {
			if !waiting[k] {
				continue
			}
			if c, ok := v.freeCell(taken, v.monitors[0], 0, 0); ok {
				place(k, c)
			}
		}
	}
	if len(unplaced) > 0 {
		v.save()
	}
}

func (v *view) looseCell(key string, c Cell) *cell {
	mon := v.monitor(c.Monitor)
	m := v.metrics(mon.Scale)
	return &cell{item: v.items[key], rect: v.cellRect(c, mon, m), m: m}
}

func (v *view) cellRect(c Cell, mon platform.DesktopMonitor, m metrics) platform.Box {
	return platform.Box{X: mon.Work.X + m.margin + float64(c.Col)*m.cellW, Y: mon.Work.Y + m.margin + float64(c.Row)*m.cellH, Width: m.cellW, Height: m.cellH}
}

// gridSize is how many columns and rows of cells a monitor holds.
func (v *view) gridSize(mon platform.DesktopMonitor, m metrics) (int, int) {
	return max(1, int((mon.Work.Width-2*m.margin)/m.cellW)), max(1, int((mon.Work.Height-2*m.margin)/m.cellH))
}

// cellFree reports whether a cell is on its monitor's grid and outside
// every box but skip.
func (v *view) cellFree(c Cell, skip *Box) bool {
	mon := v.monitor(c.Monitor)
	if mon.ID != c.Monitor {
		return false
	}
	m := v.metrics(mon.Scale)
	cols, rows := v.gridSize(mon, m)
	if c.Col < 0 || c.Row < 0 || c.Col >= cols || c.Row >= rows {
		return false
	}
	r := v.cellRect(c, mon, m)
	for _, g := range v.boxes {
		if g.box != skip && intersects(toBox(fromBox(r).Inset(m.pad)), g.rect) {
			return false
		}
	}
	return true
}

// freeCell returns the free cell nearest to a point, or, for a point of 0,
// 0, the first free one going down the columns, as File Explorer fills
// the desktop.
func (v *view) freeCell(taken map[Cell]bool, mon platform.DesktopMonitor, x, y float64) (Cell, bool) {
	m := v.metrics(mon.Scale)
	cols, rows := v.gridSize(mon, m)
	best, bestD := Cell{}, math.Inf(1)
	for col := range cols {
		for row := range rows {
			c := Cell{Monitor: mon.ID, Col: col, Row: row}
			if taken[c] || !v.cellFree(c, nil) {
				continue
			}
			if x == 0 && y == 0 {
				return c, true
			}
			r := v.cellRect(c, mon, m)
			dx, dy := r.X+r.Width/2-x, r.Y+r.Height/2-y
			if d := dx*dx + dy*dy; d < bestD {
				best, bestD = c, d
			}
		}
	}
	return best, !math.IsInf(bestD, 1)
}

// placeLoose puts items on the desktop around a point: the first in the
// free cell nearest it, the others in the free cells nearest that.
func (v *view) placeLoose(keys []string, x, y float64) {
	v.layout.take(keys)
	taken := map[Cell]bool{}
	for _, c := range v.loose {
		if !slices.Contains(keys, c.item.Key) {
			taken[v.arr.Icons[c.item.Key]] = true
		}
	}
	mon := v.monitorAt(x, y)
	for _, k := range keys {
		c, ok := v.freeCell(taken, mon, x, y)
		if !ok {
			break
		}
		taken[c] = true
		v.arr.Icons[k] = c
	}
	v.arrange()
}

func intersects(a, b platform.Box) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

func contains(b platform.Box, x, y float64) bool {
	return x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height
}

func (v *view) geoOf(boxID string) *boxGeo {
	for _, g := range v.boxes {
		if g.box.ID == boxID {
			return g
		}
	}
	return nil
}

// insertIndex is where items dropped at a point go among a box's items.
// It is -1, the end, for a sorted box.
func (v *view) insertIndex(g *boxGeo, x, y float64) int {
	if g == nil || g.box.Sort != "" {
		return -1
	}
	for _, c := range g.cells {
		r := c.rect
		if y < r.Y || (y < r.Y+r.Height && x < r.X+r.Width/2) {
			return slices.Index(g.box.Items, c.item.Key)
		}
	}
	return -1
}

func (v *view) animHeight(a *anim) float64 {
	t := math.Min(1, float64(time.Since(a.start))/float64(animTime))
	t = 1 - (1-t)*(1-t)*(1-t) // ease out
	return a.from + (a.to-a.from)*t
}

// animateHeight rolls a box up or down, or opens a rolled-up one while it
// is hovered.
func (v *view) animateHeight(g *boxGeo, to float64) {
	from := g.rect.Height
	if from == to {
		return
	}
	v.invalidate(g.full)
	v.heights[g.box.ID] = &anim{from: from, to: to, start: time.Now()}
	v.d.Animate(true)
}

// Tick moves the animations along.
func (v *view) Tick() {
	for id, a := range v.heights {
		if time.Since(a.start) >= animTime {
			delete(v.heights, id)
		}
		if g := v.geoOf(id); g != nil {
			v.invalidate(g.full)
		}
	}
	if len(v.heights) == 0 {
		v.d.Animate(false)
	}
	v.arrange()
}

func (v *view) invalidate(b platform.Box) {
	if v.d != nil {
		v.d.Invalidate(platform.Box{X: b.X - 2, Y: b.Y - 2, Width: b.Width + 4, Height: b.Height + 4})
	}
}

func (v *view) invalidateAll() {
	for _, m := range v.monitors {
		v.d.Invalidate(m.Bounds)
	}
}

// loadIcons fetches icons off the UI thread, since thumbnails can be slow.
func (v *view) loadIcons() {
	for k := range v.icons {
		img := platform.ItemImage(k.key, k.px)
		v.d.Do(func() {
			if v.stopping {
				return
			}
			delete(v.loading, k)
			v.pixels[k] = img
			v.invalidateItem(k.key)
		})
	}
}

// icon returns the bitmap of an item's icon on a canvas, asking for it to
// be loaded when it is not yet.
func (v *view) icon(c *platform.Canvas, key string, px int) *platform.Bitmap {
	k := iconKey{key, px}
	byKey := v.bitmaps[c]
	if byKey == nil {
		byKey = map[iconKey]*platform.Bitmap{}
		v.bitmaps[c] = byKey
	}
	if b := byKey[k]; c.Usable(b) {
		return b
	}
	img, ok := v.pixels[k]
	if !ok {
		if !v.loading[k] {
			select {
			case v.icons <- k:
				v.loading[k] = true
			default:
			}
		}
		return nil
	}
	b := c.NewBitmap(img)
	byKey[k] = b
	return b
}

// forgetIcon drops an item's icon so it is loaded again.
func (v *view) forgetIcon(key string) {
	for k := range v.pixels {
		if k.key == key {
			delete(v.pixels, k)
		}
	}
	for _, byKey := range v.bitmaps {
		for k, b := range byKey {
			if k.key == key {
				b.Release()
				delete(byKey, k)
			}
		}
	}
}

// dropIcons drops every icon, for a new icon size.
func (v *view) dropIcons() {
	clear(v.pixels)
	for _, byKey := range v.bitmaps {
		for _, b := range byKey {
			b.Release()
		}
	}
	clear(v.bitmaps)
	for _, t := range v.texts {
		t.Release()
	}
	clear(v.texts)
}

func (v *view) invalidateItem(key string) {
	for _, c := range v.allCells() {
		if c.item.Key == key {
			v.invalidate(c.rect)
		}
	}
}

// allCells returns every laid out item, loose ones first.
func (v *view) allCells() []*cell {
	out := slices.Clone(v.loose)
	for _, g := range v.boxes {
		out = append(out, g.cells...)
	}
	return out
}

// text returns a cached text layout, centred in its width.
func (v *view) text(s string, width float64, size, line float64, lines int, bold bool) *platform.TextLayout {
	k := labelKey{text: s, width: int(width), size: int(size * 10), lines: lines, bold: bold}
	if t, ok := v.texts[k]; ok {
		return t
	}
	if len(v.texts) > 4000 {
		for _, t := range v.texts {
			t.Release()
		}
		clear(v.texts)
	}
	t := platform.NewTextLayout(s, platform.Font{Family: v.tokens.Fonts[0], Size: size, LineHeight: line, Bold: bold, Center: true, Lines: lines}, width)
	v.texts[k] = t
	return t
}

// selectedItems returns the selected items, in the order shown.
func (v *view) selectedItems() []platform.ShellItem {
	var out []platform.ShellItem
	for _, c := range v.containerCells(v.selBox) {
		if v.selected[c.item.Key] {
			out = append(out, c.item)
		}
	}
	return out
}

// containerCells returns the cells of a box, or the loose ones for "".
func (v *view) containerCells(boxID string) []*cell {
	if boxID == "" {
		return v.loose
	}
	if g := v.geoOf(boxID); g != nil {
		return g.cells
	}
	return nil
}

func (v *view) clearSelection() {
	for _, c := range v.containerCells(v.selBox) {
		if v.selected[c.item.Key] {
			v.invalidate(c.rect)
		}
	}
	clear(v.selected)
}

// selectOnly selects one item of a container.
func (v *view) selectOnly(boxID, key string) {
	v.clearSelection()
	v.selBox = boxID
	v.selected[key] = true
	v.anchor = key
	v.invalidateItem(key)
}
