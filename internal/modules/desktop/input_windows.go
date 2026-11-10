package desktop

import (
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/shirumesu/arubox/internal/platform"
)

type hitPart int

const (
	hitDesktop hitPart = iota
	hitItem
	hitTitle
	hitBack // a folder portal's way back to the folder it came from
	hitEdge
	hitContent
)

const (
	edgeLeft = 1 << iota
	edgeRight
	edgeTop
	edgeBottom
)

type hit struct {
	part  hitPart
	box   *boxGeo
	cell  *cell
	edges int
}

func (h hit) boxID() string {
	if h.box == nil {
		return ""
	}
	return h.box.box.ID
}

// hitTest finds what is at a point, topmost first.
func (v *view) hitTest(x, y float64) hit {
	for i := len(v.boxes) - 1; i >= 0; i-- {
		g := v.boxes[i]
		m, r := g.m, g.rect
		e := m.edge / 2
		if !contains(platform.Box{X: r.X - e, Y: r.Y - e, Width: r.Width + 2*e, Height: r.Height + 2*e}, x, y) {
			continue
		}
		edges := 0
		if x < r.X+e {
			edges |= edgeLeft
		}
		if x >= r.X+r.Width-e {
			edges |= edgeRight
		}
		if r.Height > m.title+1 {
			if y < r.Y+e {
				edges |= edgeTop
			}
			if y >= r.Y+r.Height-e {
				edges |= edgeBottom
			}
		}
		switch {
		case edges != 0:
			return hit{part: hitEdge, box: g, edges: edges}
		case contains(g.title, x, y):
			if p := v.portals[g.box.ID]; p != nil && len(p.back) > 0 && x < g.title.X+m.title {
				return hit{part: hitBack, box: g}
			}
			return hit{part: hitTitle, box: g}
		case contains(g.content, x, y):
			for _, c := range g.cells {
				if contains(c.rect, x, y) {
					return hit{part: hitItem, box: g, cell: c}
				}
			}
		}
		return hit{part: hitContent, box: g}
	}
	for _, c := range v.loose {
		if contains(c.rect, x, y) {
			return hit{part: hitItem, cell: c}
		}
	}
	return hit{part: hitDesktop}
}

type pressKind int

const (
	pressItem pressKind = iota
	pressTitle
	pressBack
	pressEdge
	pressEmpty
)

// press is a mouse button held down and what it started on.
type press struct {
	kind        pressKind
	button      int
	x0, y0      float64
	moved       bool
	box         string // the box pressed on, by ID
	start       platform.Box
	edges       int
	key         string
	ctrl, shift bool
	wasSelected bool
	hit         hit
}

// Pointer handles the mouse.
func (v *view) Pointer(e platform.PointerEvent) {
	v.lastX, v.lastY = e.X, e.Y
	if v.hidden {
		if e.Kind == platform.PointerDoubleClick && e.Button == 1 {
			v.toggleHidden()
		}
		return
	}
	switch e.Kind {
	case platform.PointerMove:
		v.move(e)
	case platform.PointerDown:
		v.down(e)
	case platform.PointerUp:
		v.up(e)
	case platform.PointerDoubleClick:
		v.doubleClick(e)
	case platform.PointerWheel:
		v.wheel(e)
	case platform.PointerLeave:
		if v.press == nil {
			v.setHover(hit{part: hitDesktop})
		}
	case platform.PointerCancel:
		if v.press != nil {
			v.d.Capture(e.X, e.Y, false)
			v.endBand()
			v.press = nil
		}
	}
}

func (v *view) down(e platform.PointerEvent) {
	h := v.hitTest(e.X, e.Y)
	p := &press{button: e.Button, x0: e.X, y0: e.Y, ctrl: e.Ctrl, shift: e.Shift, hit: h, box: h.boxID()}
	if e.Button == 2 {
		if h.part == hitItem && (!v.selected[h.cell.item.Key] || v.selBox != h.boxID()) {
			v.selectOnly(h.boxID(), h.cell.item.Key)
		}
		v.press = p
		return
	}
	if e.Button != 1 {
		return
	}
	v.d.Capture(e.X, e.Y, true)
	switch h.part {
	case hitItem:
		key := h.cell.item.Key
		p.kind, p.key = pressItem, key
		if h.boxID() != v.selBox {
			v.clearSelection()
			v.selBox = h.boxID()
		}
		p.wasSelected = v.selected[key]
		switch {
		case e.Shift:
			v.selectRange(key)
		case e.Ctrl:
		case !p.wasSelected:
			v.selectOnly(h.boxID(), key)
		}
	case hitTitle, hitEdge:
		p.kind = pressTitle
		if h.part == hitEdge {
			p.kind, p.edges = pressEdge, h.edges
		}
		p.start = h.box.full
		v.raise(h.box.box.ID)
	case hitBack:
		p.kind = pressBack
	default:
		p.kind = pressEmpty
		if !e.Ctrl {
			v.clearSelection()
		}
		v.selBox = h.boxID()
	}
	v.press = p
}

// raise draws a box above the others.
func (v *view) raise(id string) {
	i := slices.IndexFunc(v.layout.Boxes, func(b Box) bool { return b.ID == id })
	if i < 0 || i == len(v.layout.Boxes)-1 {
		return
	}
	b := v.layout.Boxes[i]
	v.layout.Boxes = append(slices.Delete(v.layout.Boxes, i, i+1), b)
	v.arrange()
	if g := v.geoOf(id); g != nil {
		v.invalidate(g.rect)
	}
	v.save()
}

// selectRange selects the items between the anchor and key in the order
// shown.
func (v *view) selectRange(key string) {
	cells := v.containerCells(v.selBox)
	a := slices.IndexFunc(cells, func(c *cell) bool { return c.item.Key == v.anchor })
	b := slices.IndexFunc(cells, func(c *cell) bool { return c.item.Key == key })
	if a < 0 {
		v.selectOnly(v.selBox, key)
		return
	}
	v.clearSelection()
	for i := min(a, b); i <= max(a, b); i++ {
		v.selected[cells[i].item.Key] = true
		v.invalidate(cells[i].rect)
	}
}

func (v *view) up(e platform.PointerEvent) {
	p := v.press
	v.press = nil
	if p == nil {
		return
	}
	if p.button == 2 {
		v.contextMenu(p.hit, e.X, e.Y, e.Shift)
		return
	}
	v.d.Capture(e.X, e.Y, false)
	switch p.kind {
	case pressItem:
		if p.moved {
			return
		}
		switch {
		case p.ctrl:
			if v.selected[p.key] {
				delete(v.selected, p.key)
			} else {
				v.selected[p.key] = true
				v.anchor = p.key
			}
			v.invalidateItem(p.key)
		case !p.shift && p.wasSelected:
			v.selectOnly(p.box, p.key)
		}
	case pressTitle, pressEdge:
		if p.moved {
			v.save()
		}
	case pressBack:
		v.goBack(p.box)
	case pressEmpty:
		band := v.band
		if p.moved && p.box == "" && len(v.selected) == 0 && band.Width >= 60*v.monitorAt(e.X, e.Y).Scale && band.Height >= 60*v.monitorAt(e.X, e.Y).Scale {
			// The band stays while the menu shows, as the box to be.
			switch v.d.Menu([]platform.MenuItem{{ID: 1, Text: "创建盒子"}, {ID: 2, Text: "创建文件夹入口"}}, e.X, e.Y) {
			case 1:
				v.createBox(band, "")
			case 2:
				if folder := v.d.PickFolder(); folder != "" {
					v.createBox(band, folder)
				}
			}
		}
		v.endBand()
	}
}

func (v *view) endBand() {
	if v.band.Width > 0 || v.band.Height > 0 {
		v.invalidate(v.band)
	}
	v.band = platform.Box{}
}

func (v *view) move(e platform.PointerEvent) {
	p := v.press
	if p == nil {
		h := v.hitTest(e.X, e.Y)
		v.setHover(h)
		v.d.SetCursor(cursorFor(h))
		return
	}
	if p.button != 1 {
		return
	}
	m := v.metrics(v.monitorAt(e.X, e.Y).Scale)
	if !p.moved {
		if math.Hypot(e.X-p.x0, e.Y-p.y0) < m.drag {
			return
		}
		p.moved = true
	}
	switch p.kind {
	case pressItem:
		v.press = nil
		v.d.Capture(e.X, e.Y, false)
		if items := v.selectedItems(); len(items) > 0 {
			v.d.Drag(items)
		}
		v.setDropBox("")
	case pressTitle:
		v.moveBox(p, e.X-p.x0, e.Y-p.y0)
	case pressEdge:
		v.resizeBox(p, e.X-p.x0, e.Y-p.y0)
	case pressEmpty:
		v.dragBand(p, e.X, e.Y)
	}
}

func cursorFor(h hit) platform.Cursor {
	if h.part != hitEdge {
		return platform.CursorArrow
	}
	switch h.edges {
	case edgeLeft, edgeRight:
		return platform.CursorSizeWE
	case edgeTop, edgeBottom:
		return platform.CursorSizeNS
	case edgeLeft | edgeTop, edgeRight | edgeBottom:
		return platform.CursorSizeNWSE
	}
	return platform.CursorSizeNESW
}

// setHover follows the pointer, and opens a rolled-up box while it is over
// it when the user wants that.
func (v *view) setHover(h hit) {
	old := v.hover
	v.hover = h
	if old.box != h.box {
		if old.box != nil {
			v.invalidate(old.box.rect)
		}
		if h.box != nil {
			v.invalidate(h.box.rect)
		}
	}
	if old.cell != h.cell {
		if old.cell != nil {
			v.invalidate(old.cell.rect)
		}
		if h.cell != nil {
			v.invalidate(h.cell.rect)
		}
	}
	if !v.settings.HoverExpand {
		return
	}
	for id := range v.peeking {
		if id != h.boxID() {
			delete(v.peeking, id)
			if g := v.geoOf(id); g != nil {
				v.animateHeight(g, g.m.title)
			}
		}
	}
	if g := h.box; g != nil && g.box.Rolled && !v.peeking[g.box.ID] && h.part == hitTitle {
		v.peeking[g.box.ID] = true
		v.animateHeight(g, g.full.Height)
	}
}

func (v *view) setDropBox(id string) {
	if v.dropBox == id {
		return
	}
	if g := v.geoOf(v.dropBox); g != nil {
		v.invalidate(g.rect)
	}
	v.dropBox = id
	if g := v.geoOf(id); g != nil {
		v.invalidate(g.rect)
	}
}

// moveBox follows a box dragged by its title.
func (v *view) moveBox(p *press, dx, dy float64) {
	g := v.geoOf(p.box)
	if g == nil {
		return
	}
	r := p.start
	r.X += dx
	r.Y += dy
	if v.settings.Snap {
		mon := v.monitorAt(r.X+r.Width/2, r.Y+r.Height/2)
		m := v.metrics(mon.Scale)
		snapped := snapMove(fromBox(r), fromBox(mon.Work).Inset(m.gap), v.otherBoxes(p.box, mon), m.reach, m.gap)
		r = toBox(snapped)
	}
	old := g.rect
	v.setPlace(p.box, r)
	v.arrange()
	v.invalidate(old)
	if g := v.geoOf(p.box); g != nil {
		v.invalidate(g.rect)
	}
}

// otherBoxes returns the rectangles of the boxes on a monitor but one.
func (v *view) otherBoxes(skip string, mon platform.DesktopMonitor) []Rect {
	var out []Rect
	for _, g := range v.boxes {
		if g.box.ID != skip && g.mon.ID == mon.ID {
			out = append(out, fromBox(g.rect))
		}
	}
	return out
}

// resizeBox follows an edge of a box being dragged.
func (v *view) resizeBox(p *press, dx, dy float64) {
	g := v.geoOf(p.box)
	if g == nil {
		return
	}
	m := g.m
	minW, minH := m.cellW+2*m.pad, m.title+m.cellH/2+m.pad
	r := p.start
	area := fromBox(g.mon.Work).Inset(m.gap)
	others := v.otherBoxes(p.box, g.mon)
	snap := func(x float64, vertical bool) float64 {
		if !v.settings.Snap {
			return x
		}
		return snapEdge(x, area, others, vertical, m.reach, m.gap)
	}
	right, bottom := r.X+r.Width, r.Y+r.Height
	if p.edges&edgeLeft != 0 {
		r.X = math.Min(snap(p.start.X+dx, true), right-minW)
		r.Width = right - r.X
	}
	if p.edges&edgeRight != 0 {
		r.Width = math.Max(minW, snap(right+dx, true)-r.X)
	}
	if p.edges&edgeTop != 0 {
		r.Y = math.Min(snap(p.start.Y+dy, false), bottom-minH)
		r.Height = bottom - r.Y
	}
	if p.edges&edgeBottom != 0 {
		r.Height = math.Max(minH, snap(bottom+dy, false)-r.Y)
	}
	old := g.rect
	v.setPlace(p.box, r)
	v.arrange()
	v.invalidate(old)
	if g := v.geoOf(p.box); g != nil {
		v.invalidate(g.rect)
	}
}

// dragBand follows a selection band and selects what it touches.
func (v *view) dragBand(p *press, x, y float64) {
	old := v.band
	v.band = toBox(span(p.x0, p.y0, x, y))
	if g := v.geoOf(p.box); g != nil {
		// A band inside a box stays inside it.
		if b, ok := clip(v.band, g.content); ok {
			v.band = b
		}
	}
	v.invalidate(old)
	v.invalidate(v.band)
	for _, c := range v.containerCells(p.box) {
		in := intersects(c.rect, v.band) && (c.box == nil || intersects(c.rect, c.box.content))
		if in != v.selected[c.item.Key] {
			if in {
				v.selected[c.item.Key] = true
			} else {
				delete(v.selected, c.item.Key)
			}
			v.invalidate(c.rect)
		}
	}
}

func clip(a, b platform.Box) (platform.Box, bool) {
	x0, y0 := math.Max(a.X, b.X), math.Max(a.Y, b.Y)
	x1, y1 := math.Min(a.X+a.Width, b.X+b.Width), math.Min(a.Y+a.Height, b.Y+b.Height)
	if x1 <= x0 || y1 <= y0 {
		return platform.Box{}, false
	}
	return platform.Box{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}

func (v *view) doubleClick(e platform.PointerEvent) {
	v.press = nil
	h := v.hitTest(e.X, e.Y)
	switch h.part {
	case hitItem:
		if p := v.portals[h.boxID()]; p != nil && h.cell.item.Folder {
			v.enterFolder(h.boxID(), h.cell.item.Key)
			return
		}
		v.openSelection()
	case hitTitle:
		v.toggleRolled(h.box)
	case hitContent:
		if p := v.portals[h.boxID()]; p != nil {
			_ = platform.OpenFile(p.path)
		}
	case hitDesktop:
		if v.settings.DoubleClickHide {
			v.toggleHidden()
		}
	}
}

func (v *view) toggleHidden() {
	v.hidden = !v.hidden
	v.clearSelection()
	v.invalidateAll()
}

func (v *view) toggleRolled(g *boxGeo) {
	b := g.box
	b.Rolled = !b.Rolled
	delete(v.peeking, b.ID)
	to := g.full.Height
	if b.Rolled {
		to = g.m.title
	}
	v.animateHeight(g, to)
	v.save()
}

func (v *view) openSelection() {
	for _, it := range v.selectedItems() {
		if err := platform.OpenItem(it); err != nil {
			v.m.log.Warn("open desktop item", "item", it.Key, "error", err)
		}
	}
}

func (v *view) wheel(e platform.PointerEvent) {
	if e.Ctrl {
		s := v.settings
		s.IconSize = max(minIconSize, min(maxIconSize, s.IconSize+int(math.Copysign(8, e.Wheel))))
		if s.IconSize != v.settings.IconSize {
			go func() { _ = v.m.setSettings(s) }()
		}
		return
	}
	h := v.hitTest(e.X, e.Y)
	g := h.box
	if g == nil || g.rect.Height <= g.m.title+1 {
		return
	}
	step := float64(v.settings.IconSize + 46)
	visible := (g.full.Height - g.m.title - g.m.pad) / g.m.scale
	limit := math.Max(0, g.contentH/g.m.scale-visible)
	v.scroll[g.box.ID] = math.Max(0, math.Min(limit, v.scroll[g.box.ID]-e.Wheel*step))
	v.arrange()
	v.invalidate(g.rect)
}

// enterFolder shows a subfolder in a portal.
func (v *view) enterFolder(boxID, path string) {
	p := v.portals[boxID]
	p.back = append(p.back, p.path)
	p.path = path
	v.showPortal(boxID, p)
}

func (v *view) goBack(boxID string) {
	p := v.portals[boxID]
	if p == nil || len(p.back) == 0 {
		return
	}
	p.path = p.back[len(p.back)-1]
	p.back = p.back[:len(p.back)-1]
	v.showPortal(boxID, p)
}

func (v *view) showPortal(boxID string, p *portal) {
	if v.selBox == boxID {
		clear(v.selected)
	}
	v.scroll[boxID] = 0
	v.loadPortal(p)
	v.watch()
	v.arrange()
	if g := v.geoOf(boxID); g != nil {
		v.invalidate(g.rect)
	}
}

// createBox makes a box, or a folder portal, over a rectangle of the
// screen, and offers to name a new box at once.
func (v *view) createBox(r platform.Box, folder string) *Box {
	mon := v.monitorAt(r.X+r.Width/2, r.Y+r.Height/2)
	m := v.metrics(mon.Scale)
	r.Width = math.Max(r.Width, m.cellW*2+2*m.pad)
	r.Height = math.Max(r.Height, m.title+m.cellH+m.pad)
	b := Box{ID: v.layout.newBoxID(), Title: "新建盒子", Folder: folder}
	if folder != "" {
		b.Title = filepath.Base(folder)
		if b.Title == "." || b.Title == `\` {
			b.Title = folder
		}
	}
	v.layout.Boxes = append(v.layout.Boxes, b)
	v.setPlace(b.ID, r)
	if folder != "" {
		p := &portal{path: folder}
		v.portals[b.ID] = p
		v.loadPortal(p)
		v.watch()
	}
	v.arrange()
	v.invalidate(r)
	v.save()
	if folder == "" {
		v.renameBox(b.ID)
	}
	return v.layout.box(b.ID)
}

// boxFromSelection puts the selected loose icons in a new box around them.
func (v *view) boxFromSelection() {
	keys := v.selectedKeys()
	var r platform.Box
	for _, c := range v.loose {
		if v.selected[c.item.Key] {
			r = union(r, c.rect)
		}
	}
	if len(keys) == 0 {
		return
	}
	m := v.metrics(v.monitorAt(r.X, r.Y).Scale)
	r = platform.Box{X: r.X - m.pad, Y: r.Y - m.title, Width: r.Width + 2*m.pad, Height: r.Height + m.title + m.pad}
	b := v.createBox(r, "")
	v.layout.put(b, keys, -1)
	v.selBox = b.ID
	v.arrange()
	v.invalidateAll()
	v.save()
}

func union(a, b platform.Box) platform.Box { return toBox(fromBox(a).Union(fromBox(b))) }

func (v *view) selectedKeys() []string {
	var keys []string
	for _, it := range v.selectedItems() {
		keys = append(keys, it.Key)
	}
	return keys
}

// deleteBox removes a box; its items go back onto the desktop beside it.
func (v *view) deleteBox(id string) {
	g := v.geoOf(id)
	if g == nil {
		return
	}
	b := *g.box
	at := g.full
	v.layout.Boxes = slices.DeleteFunc(v.layout.Boxes, func(x Box) bool { return x.ID == id })
	for _, a := range v.layout.Arrangements {
		delete(a.Boxes, id)
	}
	delete(v.portals, id)
	delete(v.scroll, id)
	if v.selBox == id {
		v.selBox = ""
		clear(v.selected)
	}
	v.arrange()
	if b.Folder == "" && len(b.Items) > 0 {
		v.placeLoose(b.Items, at.X, at.Y)
	} else {
		v.watch()
	}
	v.invalidateAll()
	v.save()
}

// Menu entries the module adds.
const (
	menuOutOfBox = iota + 1
	menuNewBoxFromSelection
	menuNewBox
	menuNewPortal
	menuRefresh
	menuDisplay
	menuPersonalize
	menuRenameBox
	menuRoll
	menuOpenFolder
	menuChangeFolder
	menuDeleteBox
	menuSortManual
	menuSortName
	menuSortType
	menuSortDate
	menuColor // and the colours after it
)

func (v *view) contextMenu(h hit, x, y float64, extended bool) {
	switch h.part {
	case hitItem:
		v.itemMenu(h, x, y, extended)
	case hitTitle, hitBack, hitEdge:
		v.boxMenu(h.box, x, y, false)
	case hitContent:
		v.boxMenu(h.box, x, y, true)
	default:
		v.pending = &placement{x: x, y: y, until: time.Now().Add(10 * time.Second)}
		own := []platform.MenuItem{
			{ID: menuNewBox, Text: "新建盒子"}, {ID: menuNewPortal, Text: "新建文件夹入口…"}, {Separator: true},
			{ID: menuRefresh, Text: "刷新"}, {ID: menuDisplay, Text: "显示设置"}, {ID: menuPersonalize, Text: "个性化"},
		}
		switch v.d.FolderMenu(platform.DesktopFolder(), own, x, y) {
		case menuNewBox:
			v.createBox(platform.Box{X: x, Y: y}, "")
		case menuNewPortal:
			if folder := v.d.PickFolder(); folder != "" {
				v.createBox(platform.Box{X: x, Y: y}, folder)
			}
		case menuRefresh:
			v.refresh()
		case menuDisplay:
			_ = platform.OpenFile("ms-settings:display")
		case menuPersonalize:
			_ = platform.OpenFile("ms-settings:personalization")
		}
	}
}

func (v *view) refresh() {
	v.dropIcons()
	v.loadItems()
	v.arrange()
	v.invalidateAll()
}

func (v *view) itemMenu(h hit, x, y float64, extended bool) {
	items := v.selectedItems()
	if len(items) == 0 {
		return
	}
	var own []platform.MenuItem
	switch {
	case h.box == nil:
		own = []platform.MenuItem{{ID: menuNewBoxFromSelection, Text: "用选中的项目新建盒子"}}
	case h.box.box.Folder == "":
		own = []platform.MenuItem{{ID: menuOutOfBox, Text: "移出盒子"}}
	}
	switch v.d.ItemMenu(items, own, x, y, extended) {
	case platform.MenuRename:
		v.renameItem(h.cell)
	case menuNewBoxFromSelection:
		v.boxFromSelection()
	case menuOutOfBox:
		at := h.box.full
		keys := v.selectedKeys()
		v.clearSelection()
		v.selBox = ""
		v.placeLoose(keys, at.X+at.Width+1, at.Y)
		for _, k := range keys {
			v.selected[k] = true
		}
		v.invalidateAll()
		v.save()
	}
}

func (v *view) boxMenu(g *boxGeo, x, y float64, background bool) {
	b := g.box
	id := b.ID
	roll := "卷起"
	if b.Rolled {
		roll = "展开"
	}
	sort := []platform.MenuItem{
		{ID: menuSortName, Text: "名称", Checked: b.Sort == "name" || b.Folder != "" && b.Sort == ""},
		{ID: menuSortType, Text: "类型", Checked: b.Sort == "type"},
		{ID: menuSortDate, Text: "修改日期", Checked: b.Sort == "date"},
	}
	if b.Folder == "" {
		sort = append([]platform.MenuItem{{ID: menuSortManual, Text: "自己排列", Checked: b.Sort == ""}}, sort...)
	}
	colors := make([]platform.MenuItem, len(boxColorNames))
	for i, c := range boxColorNames {
		colors[i] = platform.MenuItem{ID: menuColor + i, Text: c.name, Checked: b.Color == c.key}
	}
	own := []platform.MenuItem{
		{ID: menuRenameBox, Text: "重命名盒子"}, {ID: menuRoll, Text: roll},
		{Text: "排序方式", Sub: sort}, {Text: "颜色", Sub: colors},
	}
	if b.Folder != "" {
		own = append(own, platform.MenuItem{Separator: true}, platform.MenuItem{ID: menuOpenFolder, Text: "在文件资源管理器中打开"}, platform.MenuItem{ID: menuChangeFolder, Text: "更换文件夹…"})
	}
	own = append(own, platform.MenuItem{Separator: true}, platform.MenuItem{ID: menuDeleteBox, Text: "删除盒子"})
	var chosen int
	switch {
	case background && b.Folder != "":
		chosen = v.d.FolderMenu(v.portals[id].path, own, x, y)
	case background:
		// New items and pasted ones land in this box.
		v.pending = &placement{box: id, x: x, y: y, until: time.Now().Add(10 * time.Second)}
		chosen = v.d.FolderMenu(platform.DesktopFolder(), own, x, y)
	default:
		chosen = v.d.Menu(own, x, y)
	}
	b = v.layout.box(id)
	if b == nil {
		return
	}
	switch {
	case chosen == menuRenameBox:
		v.renameBox(id)
	case chosen == menuRoll:
		if g := v.geoOf(id); g != nil {
			v.toggleRolled(g)
		}
	case chosen >= menuSortManual && chosen <= menuSortDate:
		b.Sort = []string{"", "name", "type", "date"}[chosen-menuSortManual]
		v.changedBox(id)
	case chosen >= menuColor && chosen < menuColor+len(boxColorNames):
		b.Color = boxColorNames[chosen-menuColor].key
		v.changedBox(id)
	case chosen == menuOpenFolder:
		_ = platform.OpenFile(v.portals[id].path)
	case chosen == menuChangeFolder:
		if folder := v.d.PickFolder(); folder != "" {
			b.Folder, b.Title = folder, filepath.Base(folder)
			p := &portal{path: folder}
			v.portals[id] = p
			v.showPortal(id, p)
			v.save()
		}
	case chosen == menuDeleteBox:
		v.deleteBox(id)
	}
}

func (v *view) changedBox(id string) {
	v.arrange()
	if g := v.geoOf(id); g != nil {
		v.invalidate(g.rect)
	}
	v.save()
}

// renameItem edits an item's name in place.
func (v *view) renameItem(c *cell) {
	if c == nil {
		return
	}
	m, r := c.m, c.rect
	name := platform.EditName(c.item)
	selectTo := len(utf16.Encode([]rune(name)))
	if !c.item.Folder {
		if dot := strings.LastIndexByte(name, '.'); dot > 0 {
			selectTo = len(utf16.Encode([]rune(name[:dot])))
		}
	}
	labelY := r.Y + m.iconTop + m.icon + m.labelGap
	box := platform.Box{X: r.X + (r.Width-m.labelW)/2 - m.pad, Y: labelY - m.labelGap/2, Width: m.labelW + 2*m.pad, Height: 3*m.line + m.pad}
	v.editing = &editing{key: c.item.Key, box: boxID(c), known: name}
	v.invalidate(r)
	v.d.Edit(box, name, selectTo, v.editFont(m))
}

// renameBox edits a box's title in place.
func (v *view) renameBox(id string) {
	g := v.geoOf(id)
	if g == nil {
		return
	}
	m := g.m
	v.editing = &editing{box: id, known: g.box.Title}
	v.invalidate(g.title)
	r := g.title
	f := v.editFont(m)
	f.Lines = 1
	v.d.Edit(platform.Box{X: r.X + m.title, Y: r.Y + m.pad/2, Width: r.Width - 2*m.title, Height: r.Height - m.pad}, g.box.Title, len(utf16.Encode([]rune(g.box.Title))), f)
}

func (v *view) editFont(m metrics) platform.Font {
	return platform.Font{Family: v.tokens.Fonts[0], Size: m.font + m.scale, LineHeight: m.line}
}

// EditDone applies a name edited in place.
func (v *view) EditDone(text string, ok bool) {
	e := v.editing
	v.editing = nil
	if e == nil {
		return
	}
	v.invalidateAll()
	text = strings.TrimSpace(text)
	if !ok || text == "" || text == e.known {
		return
	}
	if e.key == "" {
		if b := v.layout.box(e.box); b != nil {
			b.Title = text
			v.changedBox(b.ID)
		}
		return
	}
	item, found := v.findItem(e.box, e.key)
	if !found {
		return
	}
	renamed, err := platform.RenameItem(item, text)
	if err != nil {
		v.m.log.Warn("rename desktop item", "item", item.Key, "error", err)
		return
	}
	v.layout.rename(item.Key, renamed)
	if v.selected[item.Key] {
		delete(v.selected, item.Key)
		v.selected[renamed] = true
	}
	v.save()
}

// findItem finds an item of the desktop, or of a portal.
func (v *view) findItem(boxID, key string) (platform.ShellItem, bool) {
	if p := v.portals[boxID]; p != nil {
		for _, it := range p.items {
			if it.Key == key {
				return it, true
			}
		}
		return platform.ShellItem{}, false
	}
	it, ok := v.items[key]
	return it, ok
}

// Virtual-key codes the view handles.
const (
	vkBack   = 0x08
	vkReturn = 0x0D
	vkEscape = 0x1B
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkDelete = 0x2E
	vkF2     = 0x71
	vkF5     = 0x74
)

// Key handles the keyboard.
func (v *view) Key(e platform.KeyEvent) {
	if v.hidden {
		return
	}
	items := v.selectedItems()
	switch {
	case e.Key == vkDelete && len(items) > 0:
		v.d.InvokeVerb(items, "delete", v.lastX, v.lastY)
	case e.Key == vkF2 && len(items) == 1:
		for _, c := range v.containerCells(v.selBox) {
			if c.item.Key == items[0].Key {
				v.renameItem(c)
			}
		}
	case e.Key == vkReturn && len(items) > 0:
		if p := v.portals[v.selBox]; p != nil && len(items) == 1 && items[0].Folder {
			v.enterFolder(v.selBox, items[0].Key)
			return
		}
		v.openSelection()
	case e.Key == vkBack:
		v.goBack(v.selBox)
	case e.Ctrl && e.Key == 'A':
		for _, c := range v.containerCells(v.selBox) {
			v.selected[c.item.Key] = true
			v.invalidate(c.rect)
		}
	case e.Ctrl && (e.Key == 'C' || e.Key == 'X') && len(items) > 0:
		verb := "copy"
		if e.Key == 'X' {
			verb = "cut"
		}
		v.d.InvokeVerb(items, verb, v.lastX, v.lastY)
	case e.Ctrl && e.Key == 'V':
		folder := platform.DesktopFolder()
		if p := v.portals[v.selBox]; p != nil {
			folder = p.path
		} else {
			v.pending = &placement{box: v.selBox, x: v.lastX, y: v.lastY, until: time.Now().Add(10 * time.Second)}
		}
		v.d.PasteInto(folder, v.lastX, v.lastY)
	case e.Key == vkF5:
		v.refresh()
	case e.Key == vkEscape:
		v.clearSelection()
	case e.Key >= vkLeft && e.Key <= vkDown:
		v.moveSelection(e.Key)
	}
}

// moveSelection selects the nearest item in an arrow's direction.
func (v *view) moveSelection(key uint32) {
	cells := v.containerCells(v.selBox)
	if len(cells) == 0 {
		return
	}
	var from *cell
	for _, c := range cells {
		if c.item.Key == v.anchor && v.selected[c.item.Key] {
			from = c
		}
	}
	if from == nil {
		v.selectOnly(v.selBox, cells[0].item.Key)
		return
	}
	fx, fy := from.rect.X+from.rect.Width/2, from.rect.Y+from.rect.Height/2
	var best *cell
	bestD := math.Inf(1)
	for _, c := range cells {
		dx, dy := c.rect.X+c.rect.Width/2-fx, c.rect.Y+c.rect.Height/2-fy
		var along, across float64
		switch key {
		case vkLeft:
			along, across = -dx, dy
		case vkRight:
			along, across = dx, dy
		case vkUp:
			along, across = -dy, dx
		case vkDown:
			along, across = dy, dx
		}
		if along <= 1 {
			continue
		}
		if d := along + 2*math.Abs(across); d < bestD {
			best, bestD = c, d
		}
	}
	if best != nil {
		v.selectOnly(v.selBox, best.item.Key)
	}
}

// DropAt says what a drag over a point drops onto.
func (v *view) DropAt(x, y float64, internal bool) platform.DropTarget {
	v.lastX, v.lastY = x, y
	if v.hidden {
		return platform.DropTarget{}
	}
	h := v.hitTest(x, y)
	if h.box != nil && h.box.box.Folder == "" {
		v.setDropBox(h.boxID())
	} else {
		v.setDropBox("")
	}
	if h.part == hitItem && takesDrops(h.cell.item) && (!internal || !v.selected[h.cell.item.Key]) {
		return platform.DropTarget{Kind: platform.DropItem, Item: h.cell.item}
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

// Dropped finishes a drop: the layer's own items move, and files dropped
// in are placed once they appear.
func (v *view) Dropped(x, y float64, t platform.DropTarget) {
	v.setDropBox("")
	h := v.hitTest(x, y)
	switch {
	case t.Kind == platform.DropHere:
		keys := v.selectedKeys()
		if g := h.box; g != nil && g.box.Folder == "" {
			v.layout.put(g.box, keys, v.insertIndex(g, x, y))
			v.selBox = g.box.ID
			v.arrange()
		} else {
			v.selBox = ""
			v.placeLoose(keys, x, y)
		}
		v.invalidateAll()
		v.save()
	case t.Kind == platform.DropFolder && t.Path == platform.DesktopFolder():
		v.pending = &placement{box: h.boxID(), x: x, y: y, until: time.Now().Add(10 * time.Second)}
	}
}
