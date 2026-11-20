package desktop

import (
	"cmp"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
		radius:      design.Radius * scale,
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
	// err is why the folder could not be read, shown in place of its
	// items; nil once it is read.
	err error
	// sorted are the items in the order sortedBy, kept until the items or
	// the order change, since every layout shows them.
	sorted   []platform.ShellItem
	sortedBy string
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
	settings Settings
	layout   Layout
	monitors []platform.DesktopMonitor
	arr      *Arrangement

	items   map[string]platform.ShellItem // the desktop's items by key
	order   []string                      // in the order the shell lists them
	portals map[string]*portal            // by box ID

	boxes []*boxGeo // in drawing order
	loose []*cell

	pixels     map[iconKey]platform.Image
	pixelBytes int // the size of pixels' pictures
	bitmaps    map[*platform.Canvas]map[iconKey]*platform.Bitmap
	icons      chan iconKey
	// loading are the icons asked for and not yet kept; the loader skips
	// those taken out of it. iconMu guards it, as the loader reads it.
	iconMu  sync.Mutex
	loading map[iconKey]bool
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
	mark     *dropMark   // where a drag over the layer would drop
	spring   *springWait // a drag resting over a rolled-up box
	sprung   string      // a rolled-up box a drag opened
	expanded itemRef     // the item showing its whole name, as last drawn
	dragging bool        // the layer's own drag is under way
	lastX    float64     // where the pointer last was
	lastY    float64

	refreshQueued bool
	// changedAll and changed are what a queued refresh reads again besides
	// the desktop: every portal, or those showing the items in changed, by
	// folderKey, or their folders.
	changedAll bool
	changed    map[string]bool
	saveQueued bool
	stopping   bool
	unshown    int // icons without room on the grids, as last laid out
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
	mark  *dropMark // the place among the box's items, if it has one
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
		m: m, settings: s.Settings, layout: s.Layout,
		items: map[string]platform.ShellItem{}, portals: map[string]*portal{},
		pixels: map[iconKey]platform.Image{}, bitmaps: map[*platform.Canvas]map[iconKey]*platform.Bitmap{},
		loading: map[iconKey]bool{}, icons: make(chan iconKey, 4096), texts: map[labelKey]*platform.TextLayout{},
		selected: map[string]bool{}, heights: map[string]*anim{}, peeking: map[string]bool{}, scroll: map[string]float64{},
		changed: map[string]bool{},
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
	// The loader drops what is left to load.
	v.iconMu.Lock()
	clear(v.loading)
	v.iconMu.Unlock()
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

// restore replaces the layout with a snapshot's.
func (v *view) restore(l Layout) {
	v.d.Do(func() {
		if v.stopping {
			return
		}
		v.layout = l
		v.arr = v.layout.arrangement(toMonitors(v.monitors))
		clear(v.portals)
		clear(v.selected)
		v.selBox, v.anchor = "", ""
		clear(v.heights)
		clear(v.peeking)
		clear(v.scroll)
		v.loadItems(everyPortal)
		v.arrange()
		v.watch()
		v.invalidateAll()
		v.save()
	})
}

// Attached is called on the UI thread before the monitors.
func (v *view) Attached(d *platform.Desktop) {
	v.d = d
	v.loadItems(everyPortal)
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

// everyPortal has loadItems read every portal's folder.
func everyPortal(*portal) bool { return true }

// loadItems reads the desktop's items, and the folders of the portals
// reread passes and of new ones.
func (v *view) loadItems(reread func(*portal) bool) {
	items, err := platform.DesktopItems()
	if err != nil {
		v.m.log.Warn("list desktop items", "error", err)
		return
	}
	known := v.items
	v.items = make(map[string]platform.ShellItem, len(items))
	v.order = v.order[:0]
	for _, it := range items {
		if old, ok := known[it.Key]; ok && !old.Modified.Equal(it.Modified) {
			v.forgetIcon(it.Key)
		}
		v.items[it.Key] = it
		v.order = append(v.order, it.Key)
	}
	// Renames the shell did not report, or made while arubox was not
	// running, are found by the files' IDs.
	files := v.layout.Files
	for _, r := range v.layout.follow(v.items) {
		if v.selected[r[0]] {
			delete(v.selected, r[0])
			v.selected[r[1]] = true
		}
	}
	if !maps.Equal(files, v.layout.Files) {
		v.save()
	}
	// Items new to the layout go where they were dropped or pasted;
	// renamed ones already have a place.
	var added []string
	for _, it := range items {
		if _, ok := known[it.Key]; !ok && len(known) > 0 && !v.layout.placed(it.Key) {
			added = append(added, it.Key)
		}
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
		} else if !reread(p) {
			continue
		}
		v.loadPortal(p)
	}
	v.pruneIcons()
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
	p.sorted, p.sortedBy = nil, ""
	if err != nil {
		// Logged when it starts failing, not on every refresh after.
		if p.err == nil || p.err.Error() != err.Error() {
			v.m.log.Warn("read portal folder", "folder", p.path, "error", err)
		}
		p.items, p.err = nil, err
		return
	}
	p.err = nil
	modified := make(map[string]time.Time, len(p.items))
	for _, old := range p.items {
		modified[old.Key] = old.Modified
	}
	for _, it := range items {
		if t, ok := modified[it.Key]; ok && !t.Equal(it.Modified) {
			v.forgetIcon(it.Key)
		}
	}
	p.items = items
}

// placeNew puts items that just appeared where they were dropped or
// pasted, or else in the first free place on the desktop.
func (v *view) placeNew(keys []string) {
	if p := v.pending; p != nil && time.Now().Before(p.until) {
		if b := v.layout.box(p.box); b != nil && b.Folder == "" {
			v.putAt(b, keys, p.mark)
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
// once a burst of changes is over: the desktop's, and the folders of the
// portals that show the items changed, or of every portal when those are
// not known.
func (v *view) ShellChanged(changed []string, renames [][2]string) {
	for _, r := range renames {
		if r[0] != "" && r[1] != "" {
			v.layout.rename(r[0], r[1])
			if v.selected[r[0]] {
				delete(v.selected, r[0])
				v.selected[r[1]] = true
			}
		}
	}
	if len(changed) == 0 {
		v.changedAll = true
	}
	for _, p := range changed {
		if p == "" {
			v.changedAll = true
			continue
		}
		v.changed[folderKey(p)] = true
	}
	if v.refreshQueued {
		return
	}
	v.refreshQueued = true
	time.AfterFunc(150*time.Millisecond, func() {
		v.d.Do(func() {
			v.refreshQueued = false
			all, changed := v.changedAll, v.changed
			v.changedAll, v.changed = false, map[string]bool{}
			if v.stopping {
				return
			}
			folders := v.changedFolders(changed)
			// A folder outside the file system may be named otherwise in
			// the changes, so it is read on every one.
			v.loadItems(func(p *portal) bool {
				return all || folders == nil || !filepath.IsAbs(p.path) || folders[folderKey(p.path)]
			})
			v.arrange()
			v.invalidateAll()
		})
	})
}

// changedFolders returns the folders, by folderKey, the items changed are
// in, or are, themselves, as when a portal's folder is updated or deleted.
// It returns nil when an item is neither on the desktop nor in a portal's
// folder, as through a link to the folder, so every portal is read.
func (v *view) changedFolders(changed map[string]bool) map[string]bool {
	desktop := folderKey(platform.DesktopFolder())
	folders := map[string]bool{}
	for name := range changed {
		dir := folderKey(filepath.Dir(name))
		known := name == desktop || dir == desktop || strings.HasPrefix(name, "::")
		for _, p := range v.portals {
			if k := folderKey(p.path); k == name || k == dir {
				known = true
			}
		}
		if !known {
			return nil
		}
		folders[name], folders[dir] = true, true
	}
	return folders
}

// folderKey names a folder for matching paths as Windows does, ignoring
// case.
func folderKey(path string) string { return strings.ToLower(filepath.Clean(path)) }

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
	v.arr.Edited = time.Now().Unix()
}

// arrange lays out the boxes and the loose icons.
func (v *view) arrange() {
	if len(v.monitors) == 0 {
		return
	}
	// Cleared, so the slots left over do not keep old boxes' items.
	clear(v.boxes)
	v.boxes = v.boxes[:0]
	placed := false
	for i := range v.layout.Boxes {
		b := &v.layout.Boxes[i]
		p, ok := v.arr.Boxes[b.ID]
		if !ok {
			p = v.newPlace(b.ID)
			v.arr.Boxes[b.ID] = p
			placed = true
		}
		mon := v.monitor(p.Monitor)
		g := &boxGeo{box: b, mon: mon, m: v.metrics(mon.Scale), full: placeRect(p, mon)}
		v.layoutBox(g)
		v.boxes = append(v.boxes, g)
	}
	v.arrangeLoose()
	if placed {
		v.save()
	}
}

// newPlace finds a place for a box these monitors have none for: where it
// is on the monitors most like these, or else the top right corner, moved
// clear of the boxes that have a place here.
func (v *view) newPlace(id string) Place {
	cur := toMonitors(v.monitors)
	var p Place
	ok := false
	if src := v.layout.source(v.arr, func(a *Arrangement) bool { _, ok := a.Boxes[id]; return ok }); src != nil {
		p, ok = carry(src.Boxes[id], src.Monitors, cur)
	}
	if !ok {
		mon := cur[0]
		w := mon.workDIP()
		p = Place{Monitor: mon.ID, Rect: Rect{X: w.Right() - 320, Y: w.Y + 20, W: 300, H: 240}}
	}
	mon := v.monitor(p.Monitor)
	m := v.metrics(mon.Scale)
	var others []Rect
	for _, b := range v.layout.Boxes {
		if o, ok := v.arr.Boxes[b.ID]; ok && b.ID != id && o.Monitor == mon.ID {
			others = append(others, fromBox(placeRect(o, mon)))
		}
	}
	r := freeSpot(fromBox(placeRect(p, mon)), fromBox(mon.Work), others, v.snapGap(m))
	p.Rect = Rect{(r.X - mon.Bounds.X) / mon.Scale, (r.Y - mon.Bounds.Y) / mon.Scale, r.W / mon.Scale, r.H / mon.Scale}
	return p
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
	// A box sized on the grid is a whole number of cells wide, give or
	// take rounding.
	cols := max(1, int(g.content.Width/m.cellW+1e-6))
	cw := g.content.Width / float64(cols)
	rows := (len(items) + cols - 1) / cols
	g.contentH = float64(rows) * m.cellH
	visible := g.full.Height - m.title - m.pad
	g.scroll = math.Max(0, math.Min(v.scroll[g.box.ID]*m.scale, g.contentH-visible))
	clear(g.cells)
	g.cells = g.cells[:0]
	for i, it := range items {
		r := platform.Box{X: g.content.X + float64(i%cols)*cw, Y: g.content.Y + float64(i/cols)*m.cellH - g.scroll, Width: cw, Height: m.cellH}
		g.cells = append(g.cells, &cell{item: it, rect: r, m: m, box: g})
	}
}

// boxItems returns the items a box shows, in its order. A portal's are its
// own, kept sorted, which the caller must not change.
func (v *view) boxItems(b *Box) []platform.ShellItem {
	if b.Folder != "" {
		p := v.portals[b.ID]
		if p == nil {
			return nil
		}
		mode := cmp.Or(b.Sort, "name")
		if p.sortedBy != mode {
			p.sorted, p.sortedBy = sortedItems(slices.Clone(p.items), mode), mode
		}
		return p.sorted
	}
	var items []platform.ShellItem
	for _, k := range b.Items {
		if it, ok := v.items[k]; ok {
			items = append(items, it)
		}
	}
	return sortedItems(items, b.Sort)
}

// sortedItems orders items by a sort mode, in place.
func sortedItems(items []platform.ShellItem, mode string) []platform.ShellItem {
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
	clear(v.loose)
	v.loose = v.loose[:0]
	taken := map[Cell]bool{}
	var unplaced, missing []string
	for _, k := range v.order {
		if v.layout.boxOf(k) != nil {
			continue
		}
		c, ok := v.arr.Icons[k]
		if !ok {
			missing = append(missing, k)
			continue
		}
		if taken[c] || !v.cellFree(c, nil) {
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
	// Icons new to these monitors go where they are on the monitors most
	// like these, if they have been anywhere.
	for _, k := range missing {
		if x, y, ok := v.carryIcon(k); ok {
			if c, ok := v.freeCell(taken, v.monitorAt(x, y), x, y); ok {
				place(k, c)
				continue
			}
		}
		unplaced = append(unplaced, k)
	}
	// Icons new to the layout go where File Explorer had them, in its
	// column order, so a first takeover keeps the desktop as it was; the
	// rest go in the first free cells, on the primary monitor first.
	unshown := 0
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
			// Once the grids are full, they stay full for the rest.
			if unshown == 0 {
				if c, ok := v.firstFreeCell(taken); ok {
					place(k, c)
					continue
				}
			}
			unshown++
		}
	}
	// Reported once each time the grids fill up, not on every layout.
	if unshown > 0 && v.unshown == 0 {
		v.m.log.Warn("no room left on the desktop's grid; icons not shown", "count", unshown)
	}
	v.unshown = unshown
	if len(unplaced) > 0 || len(missing) > 0 {
		v.save()
	}
}

// firstFreeCell returns the first free cell going down the columns of the
// monitors, the primary one first.
func (v *view) firstFreeCell(taken map[Cell]bool) (Cell, bool) {
	for _, mon := range v.monitors {
		if c, ok := v.freeCell(taken, mon, 0, 0); ok {
			return c, true
		}
	}
	return Cell{}, false
}

// carryIcon returns where, in screen pixels, an icon these monitors have no
// cell for is on the monitors most like these.
func (v *view) carryIcon(key string) (float64, float64, bool) {
	src := v.layout.source(v.arr, func(a *Arrangement) bool { _, ok := a.Icons[key]; return ok })
	if src == nil {
		return 0, 0, false
	}
	c := src.Icons[key]
	// The middle of the cell, in DIPs from its monitor's top left corner,
	// as a point box to carry over.
	i := slices.IndexFunc(src.Monitors, func(m Monitor) bool { return m.ID == c.Monitor })
	if i < 0 {
		return 0, 0, false
	}
	sm := v.metrics(1)
	w := src.Monitors[i].workDIP()
	pt := Rect{X: w.X + sm.margin + (float64(c.Col)+0.5)*sm.cellW, Y: w.Y + sm.margin + (float64(c.Row)+0.5)*sm.cellH}
	p, ok := carry(Place{Monitor: c.Monitor, Rect: pt}, src.Monitors, toMonitors(v.monitors))
	if !ok {
		return 0, 0, false
	}
	mon := v.monitor(p.Monitor)
	return mon.Bounds.X + p.Rect.X*mon.Scale, mon.Bounds.Y + p.Rect.Y*mon.Scale, true
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
// every box but skip, as the box is at rest: a rolled-up box open for a
// peek, or rolling, leaves the icons below it where they are.
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
	r := toBox(fromBox(v.cellRect(c, mon, m)).Inset(m.pad))
	for _, g := range v.boxes {
		rest := g.full
		if g.box.Rolled {
			rest.Height = g.m.title
		}
		if g.box != skip && intersects(r, rest) {
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
// free cell nearest it, the others in the free cells nearest that, on the
// point's monitor or, once that is full, on the others. Items there is no
// room for stay where they are. It returns the items placed.
func (v *view) placeLoose(keys []string, x, y float64) []string {
	taken := map[Cell]bool{}
	for _, c := range v.loose {
		if !slices.Contains(keys, c.item.Key) {
			taken[v.arr.Icons[c.item.Key]] = true
		}
	}
	v.arr.Edited = time.Now().Unix()
	first := v.monitorAt(x, y)
	mons := append([]platform.DesktopMonitor{first}, slices.DeleteFunc(slices.Clone(v.monitors), func(m platform.DesktopMonitor) bool { return m.ID == first.ID })...)
	var placed []string
	for _, k := range keys {
		for len(mons) > 0 {
			if c, ok := v.freeCell(taken, mons[0], x, y); ok {
				taken[c] = true
				v.arr.Icons[k] = c
				placed = append(placed, k)
				break
			}
			mons = mons[1:]
		}
	}
	v.layout.take(placed)
	v.arrange()
	return placed
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

// Tick moves the animations along. Only the boxes rolling are laid out
// again: the icons around a box keep their cells while it rolls.
func (v *view) Tick() {
	for id, a := range v.heights {
		if time.Since(a.start) >= animTime {
			delete(v.heights, id)
		}
		if g := v.geoOf(id); g != nil {
			v.layoutBox(g)
			v.invalidate(g.full)
		}
	}
	if len(v.heights) == 0 {
		v.d.Animate(false)
	}
}

func (v *view) invalidate(b platform.Box) {
	if v.d != nil {
		v.d.Invalidate(platform.Box{X: b.X - 2, Y: b.Y - 2, Width: b.Width + 4, Height: b.Height + 4})
		v.followExpanded()
	}
}

// invalidateCell redraws an item, with all the space its whole name may
// take.
func (v *view) invalidateCell(c *cell) { v.invalidate(v.cellExtent(c)) }

// itemRef names an item of a box, or a loose one for box "".
type itemRef struct{ box, key string }

// followExpanded redraws the items whose whole name starts or stops
// showing. That depends on the whole selection and on editing, not only on
// the item, and the name reaches past the item's cell, so every redraw
// checks it.
func (v *view) followExpanded() {
	var now itemRef
	if c := v.expandedCell(); c != nil {
		now = itemRef{boxID(c), c.item.Key}
	}
	if now == v.expanded {
		return
	}
	old := v.expanded
	v.expanded = now
	for _, r := range []itemRef{old, now} {
		for _, c := range v.containerCells(r.box) {
			if r.key != "" && c.item.Key == r.key {
				v.invalidateCell(c)
			}
		}
	}
}

func (v *view) invalidateAll() {
	for _, m := range v.monitors {
		v.d.Invalidate(m.Bounds)
	}
}

// iconBudget bounds the pixels kept of icons, beyond those of the items in
// sight: past it, the others, such as those of a folder scrolled past, are
// dropped and loaded again when they show.
const iconBudget = 16 << 20

// loadIcons fetches icons off the UI thread, since thumbnails can be slow,
// skipping those no longer wanted.
func (v *view) loadIcons() {
	for k := range v.icons {
		v.iconMu.Lock()
		wanted := v.loading[k]
		v.iconMu.Unlock()
		if !wanted {
			continue
		}
		img := platform.ItemImage(k.key, k.px)
		v.d.Do(func() {
			v.iconMu.Lock()
			wanted := v.loading[k]
			delete(v.loading, k)
			v.iconMu.Unlock()
			if v.stopping || !wanted {
				return
			}
			v.keepPixels(k, img)
			v.invalidateItem(k.key)
		})
	}
}

// keepPixels keeps an icon's pixels and, past the budget, drops the icons
// out of sight.
func (v *view) keepPixels(k iconKey, img platform.Image) {
	if old, ok := v.pixels[k]; ok {
		v.pixelBytes -= len(old.Pixels)
	}
	v.pixels[k] = img
	v.pixelBytes += len(img.Pixels)
	if v.pixelBytes > iconBudget {
		shown := v.iconsInSight()
		v.dropIconsWhere(func(k iconKey) bool { return !shown[k] })
	}
}

// iconsInSight returns the icons of the items drawn.
func (v *view) iconsInSight() map[iconKey]bool {
	out := map[iconKey]bool{}
	for _, c := range v.loose {
		out[iconKey{c.item.Key, int(c.m.icon)}] = true
	}
	for _, g := range v.boxes {
		if g.rect.Height <= g.m.title+1 {
			continue
		}
		for _, c := range g.cells {
			if intersects(c.rect, g.content) {
				out[iconKey{c.item.Key, int(c.m.icon)}] = true
			}
		}
	}
	return out
}

// pruneIcons drops the icons of items no longer on the desktop or in a
// portal's folder, and stops loading them.
func (v *view) pruneIcons() {
	present := make(map[string]bool, len(v.items))
	for k := range v.items {
		present[k] = true
	}
	for _, p := range v.portals {
		for _, it := range p.items {
			present[it.Key] = true
		}
	}
	gone := func(k iconKey) bool { return !present[k.key] }
	v.dropIconsWhere(gone)
	v.iconMu.Lock()
	maps.DeleteFunc(v.loading, func(k iconKey, _ bool) bool { return gone(k) })
	v.iconMu.Unlock()
}

// dropIconsWhere drops the icons, pixels and bitmaps, drop is true for.
func (v *view) dropIconsWhere(drop func(iconKey) bool) {
	for k, img := range v.pixels {
		if drop(k) {
			v.pixelBytes -= len(img.Pixels)
			delete(v.pixels, k)
		}
	}
	for _, byKey := range v.bitmaps {
		for k, b := range byKey {
			if drop(k) {
				b.Release()
				delete(byKey, k)
			}
		}
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
		v.iconMu.Lock()
		if !v.loading[k] {
			select {
			case v.icons <- k:
				v.loading[k] = true
			default:
			}
		}
		v.iconMu.Unlock()
		return nil
	}
	b := c.NewBitmap(img)
	byKey[k] = b
	return b
}

// forgetIcon drops an item's icon so it is loaded again.
func (v *view) forgetIcon(key string) {
	v.dropIconsWhere(func(k iconKey) bool { return k.key == key })
}

// dropIcons drops every icon, for a new icon size.
func (v *view) dropIcons() {
	clear(v.pixels)
	v.pixelBytes = 0
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
			v.invalidateCell(c)
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
	t := platform.NewTextLayout(s, platform.Font{Family: design.Fonts[0], Size: size, LineHeight: line, Bold: bold, Center: true, Lines: lines}, width)
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
			v.invalidateCell(c)
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
