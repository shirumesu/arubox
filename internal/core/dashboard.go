package core

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"math"
	"slices"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
	"github.com/shirumesu/arubox/internal/update"
)

// Layout of the dashboard, in DIPs, after the web version's.
const (
	sidebarW = 208
	// columnW is the widest the page column gets, its side padding
	// included.
	columnW    = 672
	columnPad  = 32
	sectionGap = 24
)

// navItem is one page in the sidebar.
type navItem struct {
	id    string
	title string
	icon  rune
	page  module.Page
	// module is the id of the module the page belongs to; "" for the
	// core's own pages.
	module string
}

// confirmation is a question the dashboard asks before doing something
// the user may not expect, such as taking a hotkey from another one.
type confirmation struct {
	title, text, label string
	run                func()
}

// dashboard is opened rarely, so its window is destroyed on close and
// created again on the next open, and everything it shows is dropped with
// it. All of its fields are touched on the UI thread only.
type dashboard struct {
	core *Core
	win  *ui.Window
	logo *ui.Image
	nav  []navItem
	// current is the id of the page shown.
	current string
	// contentH is how tall the page drew last frame, which sizes the
	// scroll area of the next.
	contentH    float64
	resetScroll bool
	toast       ui.Toast
	confirm     *confirmation
	showUpdate  bool

	modules      []module.Status
	hotkeys      []hotkey.Status
	autostart    bool
	autostartErr string
	update       update.State
	// notes is the parsed release notes of notesOf.
	notes   []noteBlock
	notesOf string

	rec recorder
}

func newDashboard(c *Core) *dashboard {
	d := &dashboard{core: c, current: "general", update: c.updater.State()}
	c.updater.OnChange(func(s update.State) {
		ui.Post(func() {
			d.update = s
			if s.Release == nil {
				d.showUpdate = false
			}
			d.invalidate()
		})
	})
	return d
}

// open shows the dashboard, creating its window if it is closed.
func (d *dashboard) open() {
	if d.win != nil {
		d.win.Restore()
		d.win.Show(true)
		return
	}
	d.refresh()
	d.update = d.core.updater.State()
	d.toast, d.confirm, d.showUpdate, d.contentH = ui.Toast{}, nil, false, 0
	d.buildNav()
	w, err := ui.NewWindow(ui.WindowOptions{
		Kind:       ui.Normal,
		Title:      appName,
		Bounds:     ui.Bounds{W: 960, H: 640},
		SizeInDIPs: true,
		Center:     true,
		MinW:       760,
		MinH:       520,
		Taskbar:    true,
		Resizable:  true,
		Icon:       appIcon,
	}, ui.ViewFunc(d.frame))
	if err != nil {
		d.core.log.Error("open dashboard", "error", err)
		d.nav = nil
		return
	}
	w.OnClose = func() bool { return true }
	// Clicking away from a hotkey being recorded cancels it, as switching
	// to another window does.
	w.OnDeactivate = d.endRecording
	w.OnDestroy = func() {
		d.endRecording()
		d.win = nil
		d.logo.Release()
		d.logo = nil
		d.nav, d.modules, d.hotkeys, d.notes, d.notesOf = nil, nil, nil, nil, ""
	}
	d.win = w
	d.logo = iconImage(appIcon, int(math.Ceil(32*w.Scale())))
	d.opened()
	w.Show(true)
}

// openUpdate opens the dashboard on the update dialog.
func (d *dashboard) openUpdate() {
	d.open()
	if d.win != nil {
		d.showUpdate = true
		d.win.Invalidate()
	}
}

func (d *dashboard) invalidate() {
	if d.win != nil {
		d.win.Invalidate()
	}
}

// buildNav lists the core's pages and then the pages of the modules.
func (d *dashboard) buildNav() {
	d.nav = []navItem{
		{id: "general", title: "通用", icon: ui.IconSettings, page: generalPage{d}},
		{id: "modules", title: "模块", icon: ui.IconApps, page: modulesPage{d}},
		{id: "hotkeys", title: "快捷键", icon: ui.IconKeyboard, page: hotkeysPage{d}},
		{id: "ocr", title: "文字识别", icon: ui.IconRead, page: &ocrPage{d: d}},
	}
	for _, m := range d.core.modules.Modules() {
		if p := m.Settings(); p != nil {
			info := m.Info()
			d.nav = append(d.nav, navItem{id: info.ID, title: info.Name, icon: info.Icon, page: p, module: info.ID})
		}
	}
}

// page returns the page shown.
func (d *dashboard) page() navItem {
	if i := slices.IndexFunc(d.nav, func(n navItem) bool { return n.id == d.current }); i >= 0 {
		return d.nav[i]
	}
	return d.nav[0]
}

func (d *dashboard) selectPage(id string) {
	if id == d.current {
		return
	}
	d.current = id
	d.resetScroll = true
	d.refresh()
	d.opened()
}

// opened tells the page shown that it is, so it can load fresh state.
func (d *dashboard) opened() {
	if o, ok := d.page().page.(module.Opener); ok && d.win != nil {
		o.Opened(d.win)
	}
}

// refresh reads the state the core's pages show.
func (d *dashboard) refresh() {
	c := d.core
	d.modules = c.modules.List()
	d.hotkeys = c.hotkeys.List()
	on, err := platform.AutostartEnabled()
	d.autostart, d.autostartErr = on, ""
	if err != nil {
		d.autostartErr = err.Error()
	}
}

// fail shows err, if any, as a toast.
func (d *dashboard) fail(err error) {
	if err != nil && d.win != nil {
		d.toast.Show(d.win, err.Error(), true, 5000)
	}
}

func (d *dashboard) setAutostart(on bool) {
	err := platform.SetAutostart(on)
	if err != nil {
		d.core.log.Error("set autostart", "enabled", on, "error", err)
	}
	d.refresh()
	d.fail(err)
}

func (d *dashboard) setModuleEnabled(id string, on bool) {
	err := d.core.modules.SetEnabled(id, on)
	d.refresh()
	d.fail(err)
}

// frame draws the dashboard: the sidebar, the page in a scrolled column,
// and over them the dialogs and the toast.
func (d *dashboard) frame(f *ui.Frame) {
	if len(d.nav) == 0 {
		return
	}
	d.rec.hit = false
	pressed := f.Event().Kind == ui.EvMouseDown

	_, h := f.Size()
	d.sidebar(f, h)

	w, _ := f.Size()
	area := ui.Rect{X: sidebarW, Y: 16, W: w - sidebarW, H: h - 16}
	if d.resetScroll {
		f.ResetScroll("page")
		d.resetScroll = false
	}
	item := d.page()
	var height float64
	f.Scroll("page", area, d.contentH, func(r ui.Rect) {
		col := math.Min(r.W, columnW) - 2*columnPad
		x := r.X + (r.W-col)/2
		y := r.Y
		f.Text(ui.Rect{X: x, Y: y, W: col, H: 28}, item.title, ui.Text{Size: 20, Weight: ui.Semibold, Color: f.Theme.Foreground, Middle: true})
		y += 28 + sectionGap
		y = d.drawPage(f, item, x, y, col)
		height = y + 40 - r.Y
	})
	if height != d.contentH {
		d.contentH = height
		f.Invalidate()
	}

	d.drawUpdateDialog(f)
	d.drawConfirm(f)
	d.toast.Draw(f)

	if pressed && d.rec.stop != nil && !d.rec.hit {
		d.endRecording()
	}
}

func (d *dashboard) sidebar(f *ui.Frame, h float64) {
	t := f.Theme
	f.Fill(ui.Rect{W: sidebarW, H: h}, t.Sidebar)
	f.Fill(ui.Rect{X: sidebarW - 1/f.Scale, W: 1 / f.Scale, H: h}, t.SidebarBorder)
	if d.logo != nil {
		f.Image(d.logo, ui.Rect{X: 12, Y: 12, W: 32, H: 32}, ui.Rect{}, 1, true)
	}
	f.Text(ui.Rect{X: 50, Y: 16, W: sidebarW - 60, H: 24}, appName, ui.Text{Size: 15, Weight: ui.Semibold, Color: t.Foreground, Middle: true})
	y := 56.0
	for i, it := range d.nav {
		if it.module != "" && (i == 0 || d.nav[i-1].module == "") {
			y += 16
			f.Text(ui.Rect{X: 18, Y: y, W: sidebarW - 36, H: 16}, "工具", ui.Text{Size: 12, Weight: ui.Medium, Color: t.MutedForeground, Middle: true})
			y += 20
		}
		r := ui.Rect{X: 8, Y: y, W: sidebarW - 16, H: 32}
		active := it.id == d.page().id
		res := f.Interact("nav."+it.id, r)
		f.SetCursor(r, ui.CursorHand)
		fg := t.SidebarForeground
		switch {
		case active:
			f.FillRound(r, 6, t.SidebarAccent)
			fg = t.SidebarAccentForeground
		case res.Hovered:
			f.FillRound(r, 6, t.SidebarAccent.WithAlpha(t.SidebarAccent.A*0.6))
			fg = t.SidebarAccentForeground
		}
		iconCol, weight := fg, ui.Regular
		if active {
			iconCol, weight = t.Primary, ui.Medium
		}
		f.Icon(ui.Rect{X: r.X + 10, Y: r.Y, W: 16, H: r.H}, it.icon, 15, iconCol)
		f.Text(ui.Rect{X: r.X + 36, Y: r.Y, W: r.W - 44, H: r.H}, it.title, ui.Text{Size: 14, Weight: weight, Color: fg, Middle: true})
		if res.Clicked {
			d.selectPage(it.id)
		}
		y += 34
	}
}

// drawPage draws a page. A module's page gets the switch that enables the
// module above it, as the module cannot reach the registry itself.
func (d *dashboard) drawPage(f *ui.Frame, item navItem, x, y, w float64) float64 {
	if item.module != "" {
		if i := slices.IndexFunc(d.modules, func(m module.Status) bool { return m.ID == item.module }); i >= 0 {
			m := d.modules[i]
			y = f.Section(x, y, w, "模块", []ui.Row{{
				Title: "启用" + m.Name, Description: m.Description, Note: m.Error,
				ControlW: 36,
				Control: func(r ui.Rect) {
					on := m.Enabled
					if f.Switch("module."+m.ID, r, &on, false) {
						d.setModuleEnabled(m.ID, on)
					}
				},
			}})
			y += sectionGap
		}
	}
	return item.page.Draw(f, x, y, w)
}

// drawConfirm draws the question being asked, if any.
func (d *dashboard) drawConfirm(f *ui.Frame) {
	c := d.confirm
	if c == nil {
		return
	}
	t := f.Theme
	const boxW = 420
	textStyle := ui.Text{Size: 13, Wrap: true, Color: t.MutedForeground}
	_, th := ui.MeasureText(c.text, textStyle, boxW-48)
	h := 48 + 22 + 8 + th + 24 + 32
	dismissed := f.Dialog("confirm", boxW, h, func(r ui.Rect) {
		f.Text(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 22}, c.title, ui.Text{Size: 16, Weight: ui.Semibold, Color: t.Foreground, Middle: true})
		f.Text(ui.Rect{X: r.X, Y: r.Y + 30, W: r.W, H: th}, c.text, textStyle)
		ok := ui.Button{Label: c.label, Kind: ui.ButtonPrimary}
		cancel := ui.Button{Label: "取消"}
		okW, cancelW := ok.Width(), cancel.Width()
		by := r.Bottom() - 32
		if f.Button("confirm.cancel", ui.Rect{X: r.Right() - okW - 8 - cancelW, Y: by, W: cancelW, H: 32}, cancel) {
			d.confirm = nil
		}
		if f.Button("confirm.ok", ui.Rect{X: r.Right() - okW, Y: by, W: okW, H: 32}, ok) {
			d.confirm = nil
			c.run()
		}
	})
	if dismissed || f.KeyPressed(ui.KeyEscape, 0) {
		d.confirm = nil
	}
}

// iconImage decodes the entry of an ICO file that best suits px pixels.
// Every entry of the program's icon is a PNG.
func iconImage(ico []byte, px int) *ui.Image {
	if len(ico) < 6 {
		return nil
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	// The smallest entry at least px wide, else the largest.
	best, bestW := -1, 0
	for i := 0; i < n && 6+16*(i+1) <= len(ico); i++ {
		w := int(ico[6+16*i])
		if w == 0 {
			w = 256
		}
		better := w > bestW
		if bestW >= px {
			better = w >= px && w < bestW
		}
		if best < 0 || better {
			best, bestW = i, w
		}
	}
	if best < 0 {
		return nil
	}
	e := ico[6+16*best:]
	size, off := int(binary.LittleEndian.Uint32(e[8:])), int(binary.LittleEndian.Uint32(e[12:]))
	if off < 0 || size < 0 || off+size > len(ico) {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(ico[off : off+size]))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	pix := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			// RGBA gives the colour premultiplied, as Direct2D wants it.
			r, g, bl, a := img.At(x, y).RGBA()
			pix = append(pix, byte(bl>>8), byte(g>>8), byte(r>>8), byte(a>>8))
		}
	}
	return ui.NewImage(b.Dx(), b.Dy(), pix)
}
