package launcher

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/ui"
)

const (
	// notEditing is settingsPage.editing when no website is being edited;
	// -1 is a new one.
	notEditing = -2
	// statusEvery is how often the page reads the file index's progress.
	statusEvery = 1500 * time.Millisecond
	sectionGap  = 24.0
)

// settingsPage is the module's page in the dashboard. The dashboard draws
// the switch that enables the module above it. Everything in it belongs to
// the UI thread.
type settingsPage struct {
	m *Module
	// errs are the save errors shown under the row that failed, by row.
	errs map[string]string

	status   FileIndexStatus
	statusAt time.Time
	// ticking is set while a repaint is scheduled on tickWin, so the index
	// progress stays current while the page shows.
	ticking bool
	tickWin *ui.Window
	busy    bool // the index service is being installed or removed

	excludes      draft
	translateWord draft
	filesWord     draft

	// editing is the index of the website being edited, -1 for a new one,
	// or notEditing.
	editing int
	form    [3]ui.Field // name, keyword, URL
}

// draft is a text box that saves when it loses the focus or on Enter, and
// shows the saved value again otherwise.
type draft struct {
	field   ui.Field
	focused bool
	synced  bool
	base    string
}

func newSettingsPage(m *Module) *settingsPage {
	p := &settingsPage{m: m, errs: map[string]string{}, editing: notEditing}
	p.excludes.field = ui.Field{Multiline: true, Size: 12.5}
	p.translateWord.field = ui.Field{Size: 13, Placeholder: "已关闭"}
	p.filesWord.field = ui.Field{Size: 13, Placeholder: "已关闭"}
	p.form[0] = ui.Field{Size: 13, Placeholder: "Google"}
	p.form[1] = ui.Field{Size: 13, Placeholder: "gg"}
	p.form[2] = ui.Field{Size: 13, Placeholder: "https://www.google.com/search?q={query}"}
	return p
}

// Opened starts the page afresh each time the dashboard shows it.
func (p *settingsPage) Opened(*ui.Window) {
	clear(p.errs)
	p.editing = notEditing
	p.statusAt = time.Time{}
	for _, d := range []*draft{&p.excludes, &p.translateWord, &p.filesWord} {
		d.synced = false
	}
}

// save stores new settings, recording the error under row when it fails.
func (p *settingsPage) save(row string, s Settings) bool {
	if err := p.m.setSettings(s); err != nil {
		p.errs[row] = err.Error()
		return false
	}
	delete(p.errs, row)
	// File search may have been turned on or off.
	p.statusAt = time.Time{}
	return true
}

// Draw draws the page in the column x..x+w from y.
func (p *settingsPage) Draw(f *ui.Frame, x, y, w float64) float64 {
	s := p.m.currentSettings()
	p.pollStatus(f.Window())
	y = p.drawSearch(f, x, y, w, s) + sectionGap
	y = p.drawFileIndex(f, x, y, w, s) + sectionGap
	y = p.drawKeywords(f, x, y, w, s) + sectionGap
	return p.drawSites(f, x, y, w, s)
}

// pollStatus reads the file index's progress every statusEvery while the
// page is drawn, and schedules the repaint that shows it.
func (p *settingsPage) pollStatus(w *ui.Window) {
	if time.Since(p.statusAt) >= statusEvery {
		// The page shows the index's progress, so it keeps the index
		// loaded while it is drawn.
		if err := p.m.useFiles(); err != nil {
			p.m.log.Error("start file index", "error", err)
		}
		p.status, p.statusAt = p.m.fileIndexStatus(), time.Now()
	}
	if p.ticking && p.tickWin == w {
		return
	}
	// A timer of a closed dashboard never fires, so a new window starts
	// its own.
	p.ticking, p.tickWin = true, w
	w.After(int(statusEvery/time.Millisecond), func() {
		if p.tickWin == w {
			p.ticking = false
		}
		w.Invalidate()
	})
}

func (p *settingsPage) drawSearch(f *ui.Frame, x, y, w float64, s Settings) float64 {
	return f.Section(x, y, w, "搜索", []ui.Row{
		{
			Title:       "搜索文件和文件夹",
			Description: "按名称查找所有硬盘上的文件，支持 Everything 的搜索语法，如 *.pdf、ext:docx、folder:、!排除词、a | b。",
			Note:        p.errs["files"],
			ControlW:    36, ControlH: 20,
			Control: func(r ui.Rect) {
				on := s.Files
				if f.Switch("launcher.files", r, &on, false) {
					next := s
					next.Files = on
					p.save("files", next)
				}
			},
		},
		{
			Title:       "本币",
			Description: "输入“100 usd”这样的金额时换算成这种货币，也会给出美元或欧元。汇率来自欧洲央行，每个工作日更新。",
			Note:        p.errs["currency"],
			ControlW:    128,
			Control: func(r ui.Rect) {
				i := slices.Index(currencies, s.Currency)
				if f.Select("launcher.currency", r, currencies, &i) {
					next := s
					next.Currency = currencies[i]
					p.save("currency", next)
				}
			},
		},
	})
}

// groupDigits writes n with thousands separators, as 12,345.
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func indexDescription(st FileIndexStatus) string {
	files := groupDigits(st.Files)
	switch st.State {
	case "off":
		return "开启启动器后可用。"
	case "idle":
		return "启动器一段时间没用，文件列表已从内存释放，打开启动器时会自动重新载入。"
	case "unused":
		return "文件搜索已关闭：打开上面的“搜索文件和文件夹”，或设置“搜索文件”关键词后可用。"
	case "missing":
		return "安装后，输入名称就能立刻找到所有硬盘上的文件。安装时需要管理员授权一次。"
	case "stopped":
		return "文件搜索没有在运行，重新安装即可恢复。"
	case "outdated":
		return "文件搜索需要更新，请重新安装。"
	case "connecting":
		return "正在启动……"
	case "indexing":
		return "正在整理文件列表，已找到 " + files + " 项……"
	case "ready":
		d := "可搜索 " + files + " 个文件和文件夹，有变化会自动更新。"
		if len(st.Volumes) > 0 {
			d += "（" + strings.Join(st.Volumes, "  ") + "）"
		}
		return d
	}
	return ""
}

func (p *settingsPage) drawFileIndex(f *ui.Frame, x, y, w float64, s Settings) float64 {
	st := p.status
	service := ui.Row{
		Title:       "索引服务",
		Description: indexDescription(st),
		Note:        strings.TrimSpace(st.Message + " " + p.errs["index"]),
	}
	var button ui.Button
	install := false
	switch st.State {
	case "missing", "stopped", "outdated":
		button = ui.Button{Label: "重新安装", Kind: ui.ButtonPrimary, Small: true, Disabled: p.busy}
		if st.State == "missing" {
			button.Label = "安装"
		}
		install = true
	case "ready", "indexing", "connecting":
		button = ui.Button{Label: "移除", Kind: ui.ButtonGhost, Small: true, Disabled: p.busy}
	}
	if button.Label != "" {
		service.ControlW = button.Width()
		service.Control = func(r ui.Rect) {
			if f.Button("launcher.index", r, button) {
				p.changeIndex(f.Window(), install)
			}
		}
	}

	lines := max(2, len(s.ExcludedFolders)+1)
	boxH := float64(lines)*17 + 14
	p.excludes.sync(strings.Join(s.ExcludedFolders, "\r\n"))
	excludes := ui.Row{
		Title:       "不搜索的文件夹",
		Description: "系统文件夹、回收站和各类缓存已经自动跳过。每行写一个你不想搜到的文件夹：完整路径只跳过这一个，只写名字会跳过所有同名文件夹。",
		Note:        p.errs["excludes"],
		ExtraH:      boxH + 4,
		Extra: func(r ui.Rect) {
			p.excludes.box(f, "launcher.excludes", ui.Rect{X: r.X, Y: r.Y, W: r.W, H: boxH}, func(text string) {
				var list []string
				for _, l := range strings.Split(text, "\n") {
					if l = strings.TrimSpace(l); l != "" {
						list = append(list, l)
					}
				}
				if slices.Equal(list, s.ExcludedFolders) {
					return
				}
				next := s
				next.ExcludedFolders = list
				p.save("excludes", next)
			})
		},
	}
	return f.Section(x, y, w, "文件索引", []ui.Row{service, excludes})
}

// changeIndex installs or removes the index service. Both wait for the
// user to grant administrator rights, so they run on a goroutine.
func (p *settingsPage) changeIndex(w *ui.Window, install bool) {
	p.busy = true
	go func() {
		err := p.m.changeFileIndex(install)
		ui.Post(func() {
			p.busy = false
			p.statusAt = time.Time{}
			if err != nil {
				p.errs["index"] = err.Error()
			} else {
				delete(p.errs, "index")
			}
			if !w.Closed() {
				w.Invalidate()
			}
		})
	}()
}

func (p *settingsPage) drawKeywords(f *ui.Frame, x, y, w float64, s Settings) float64 {
	or := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	p.translateWord.sync(s.TranslateKeyword)
	p.filesWord.sync(s.FilesKeyword)
	word := func(d *draft, id, row string, set func(*Settings, string)) func(r ui.Rect) {
		return func(r ui.Rect) {
			d.box(f, id, r, func(text string) {
				text = strings.TrimSpace(text)
				next := s
				set(&next, text)
				if next.TranslateKeyword == s.TranslateKeyword && next.FilesKeyword == s.FilesKeyword {
					return
				}
				p.save(row, next)
			})
		}
	}
	return f.Section(x, y, w, "关键词", []ui.Row{
		{
			Title:       "默认搜索",
			Description: "输入任何内容时，结果的最后一项都可以用它在网上搜索。",
			Note:        p.errs["default"],
			ControlW:    144,
			Control: func(r ui.Rect) {
				options := []string{"不提供"}
				i := 0
				for j, k := range s.Keywords {
					options = append(options, k.Name)
					if strings.EqualFold(k.Keyword, s.DefaultSearch) {
						i = j + 1
					}
				}
				if f.Select("launcher.default", r, options, &i) {
					next := s
					next.DefaultSearch = ""
					if i > 0 {
						next.DefaultSearch = s.Keywords[i-1].Keyword
					}
					p.save("default", next)
				}
			},
		},
		{
			Title:       "翻译",
			Description: "输入“" + or(s.TranslateKeyword, "fy") + " 文字”，总是把后面的内容翻译成目标语言。留空关闭。",
			Note:        p.errs["translate"],
			ControlW:    96,
			Control:     word(&p.translateWord, "launcher.translateWord", "translate", func(s *Settings, v string) { s.TranslateKeyword = v }),
		},
		{
			Title:       "搜索文件",
			Description: "输入“" + or(s.FilesKeyword, "f") + " 名称”，只搜索文件和文件夹；关掉上面的“搜索文件和文件夹”时也能用。留空关闭。",
			Note:        p.errs["filesWord"],
			ControlW:    96,
			Control:     word(&p.filesWord, "launcher.filesWord", "filesWord", func(s *Settings, v string) { s.FilesKeyword = v }),
		},
	})
}

// cardItem is one band of a card: its height and what draws it.
type cardItem struct {
	h    float64
	draw func(r ui.Rect)
}

// card draws a titled card of bands with lines between them, as Section
// does for rows, and returns the y below it.
func card(f *ui.Frame, x, y, w float64, title string, items []cardItem) float64 {
	t := f.Theme
	f.Text(ui.Rect{X: x + 4, Y: y, W: w, H: 18}, title, ui.Text{Size: 12, Weight: ui.Medium, Color: t.MutedForeground, Middle: true})
	y += 26
	total := 0.0
	for _, it := range items {
		total += it.h
	}
	f.FillRound(ui.Rect{X: x, Y: y, W: w, H: total}, design.Radius, t.Card)
	f.StrokeRound(ui.Rect{X: x, Y: y, W: w, H: total}, design.Radius, 1, t.Border)
	iy := y
	for i, it := range items {
		if i > 0 {
			f.Fill(ui.Rect{X: x + 1, Y: iy, W: w - 2, H: 1 / f.Scale}, t.Border)
		}
		it.draw(ui.Rect{X: x, Y: iy, W: w, H: it.h})
		iy += it.h
	}
	return y + total
}

const sitesIntro = "在启动器里输入关键词和空格，关键词变成标签，再输入内容、回车，就在默认浏览器里搜索；不输入内容直接回车打开网站首页。"

func (p *settingsPage) drawSites(f *ui.Frame, x, y, w float64, s Settings) float64 {
	t := f.Theme
	sites := s.Keywords
	if p.editing >= len(sites) {
		p.editing = notEditing
	}
	introText := ui.Text{Size: 13, Wrap: true, Color: t.MutedForeground}
	_, ih := ui.MeasureText(sitesIntro, introText, w-32)
	noteText := ui.Text{Size: 13, Wrap: true, Color: t.Destructive}
	var nh float64
	if e := p.errs["sites"]; e != "" {
		_, nh = ui.MeasureText(e, noteText, w-32)
		nh += 4
	}
	items := []cardItem{{h: 12 + ih + nh + 4, draw: func(r ui.Rect) {
		f.Text(ui.Rect{X: r.X + 16, Y: r.Y + 12, W: r.W - 32, H: ih}, sitesIntro, introText)
		if nh > 0 {
			f.Text(ui.Rect{X: r.X + 16, Y: r.Y + 16 + ih, W: r.W - 32, H: nh}, p.errs["sites"], noteText)
		}
	}}}
	for i, k := range sites {
		if p.editing == i {
			items = append(items, p.formItem(f, s, i))
			continue
		}
		items = append(items, cardItem{h: 60, draw: func(r ui.Rect) { p.drawSite(f, r, s, i, k) }})
	}
	if p.editing == -1 {
		items = append(items, p.formItem(f, s, -1))
	} else {
		items = append(items, cardItem{h: 52, draw: func(r ui.Rect) {
			add := ui.Button{Label: "添加网站", Icon: ui.IconAdd, Small: true}
			if f.Button("launcher.site.add", ui.Rect{X: r.X + 16, Y: r.Y + 10, W: add.Width(), H: 32}, add) {
				p.startEdit(-1, Keyword{})
			}
			restore := ui.Button{Label: "恢复默认网站", Icon: ui.IconRefresh, Kind: ui.ButtonGhost, Small: true}
			rw := restore.Width()
			if f.Button("launcher.site.restore", ui.Rect{X: r.Right() - 16 - rw, Y: r.Y + 10, W: rw, H: 32}, restore) {
				p.restoreSites(s)
			}
		}})
	}
	return card(f, x, y, w, "网站", items)
}

func (p *settingsPage) drawSite(f *ui.Frame, r ui.Rect, s Settings, i int, k Keyword) {
	t := f.Theme
	textW := r.W - 32 - 2*32 - 4 - 16
	name := ui.Text{Size: 14, Weight: ui.Medium, Color: t.Foreground, Middle: true}
	nw, _ := ui.MeasureText(k.Name, name, textW)
	nw = min(nw, textW-40)
	f.Text(ui.Rect{X: r.X + 16, Y: r.Y + 11, W: nw + 1, H: 20}, k.Name, name)
	mono := ui.Text{Size: 11, Family: "Consolas", Color: t.MutedForeground, Middle: true}
	kw, _ := ui.MeasureText(k.Keyword, mono, 0)
	pill := ui.Rect{X: r.X + 16 + nw + 8, Y: r.Y + 13, W: kw + 12, H: 16}
	f.FillRound(pill, 4, t.Muted)
	f.Text(pill, k.Keyword, ui.Text{Size: 11, Family: "Consolas", Color: t.MutedForeground, Align: ui.AlignCenter, Middle: true})
	f.Text(ui.Rect{X: r.X + 16, Y: r.Y + 33, W: textW, H: 16}, k.URL, ui.Text{Size: 12, Family: "Consolas", Color: t.MutedForeground, Middle: true})

	bx := r.Right() - 16 - 32
	by := r.Y + (r.H-32)/2
	if f.Button(fmt.Sprintf("launcher.site.delete.%d", i), ui.Rect{X: bx, Y: by, W: 32, H: 32}, ui.Button{Icon: ui.IconDelete, Kind: ui.ButtonGhost}) {
		p.editing = notEditing
		p.saveSites(s, slices.Delete(slices.Clone(s.Keywords), i, i+1), k.Keyword, "", "sites")
	}
	if f.Button(fmt.Sprintf("launcher.site.edit.%d", i), ui.Rect{X: bx - 36, Y: by, W: 32, H: 32}, ui.Button{Icon: ui.IconPen, Kind: ui.ButtonGhost}) {
		p.startEdit(i, k)
	}
}

func (p *settingsPage) startEdit(i int, k Keyword) {
	p.editing = i
	p.form[0].Text, p.form[1].Text, p.form[2].Text = k.Name, k.Keyword, k.URL
	p.form[0].Focus = true
	delete(p.errs, "form")
}

// saveSites saves the websites; the default search follows its keyword
// when it is renamed or removed.
func (p *settingsPage) saveSites(s Settings, next []Keyword, from, to, row string) bool {
	def := s.DefaultSearch
	if from != "" && strings.EqualFold(def, from) {
		def = to
	}
	s.Keywords, s.DefaultSearch = next, def
	return p.save(row, s)
}

// restoreSites brings back the websites the launcher came with, as they
// were; the user's own stay.
func (p *settingsPage) restoreSites(s Settings) {
	p.editing = notEditing
	next := slices.Clone(defaultKeywords)
	for _, k := range s.Keywords {
		if !slices.ContainsFunc(defaultKeywords, func(d Keyword) bool { return strings.EqualFold(d.Keyword, k.Keyword) }) {
			next = append(next, k)
		}
	}
	s.Keywords = next
	if s.DefaultSearch == "" {
		s.DefaultSearch = "gg"
	}
	p.save("sites", s)
}

// formItem edits website i, or a new one for -1. Enter saves, Esc cancels.
func (p *settingsPage) formItem(f *ui.Frame, s Settings, i int) cardItem {
	return cardItem{h: 196, draw: func(r ui.Rect) {
		t := f.Theme
		f.FillRound(r.InsetXY(4, 4), design.Radius-4, t.Muted.WithAlpha(0.3))
		ids := [3]string{"launcher.form.name", "launcher.form.keyword", "launcher.form.url"}
		ev := f.Event()
		if ev.Kind == ui.EvKeyDown && slices.Contains(ids[:], ev.ID) {
			if f.KeyPressed(ui.KeyReturn, 0) {
				p.saveForm(s, i)
				return
			}
			if f.KeyPressed(ui.KeyEscape, 0) {
				p.editing = notEditing
				return
			}
		}
		label := ui.Text{Size: 12, Color: t.MutedForeground, Middle: true}
		x, y := r.X+16, r.Y+12
		f.Text(ui.Rect{X: x, Y: y, W: 160, H: 16}, "名称", label)
		f.Text(ui.Rect{X: x + 168, Y: y, W: 128, H: 16}, "关键词", label)
		inputBox(f, ids[0], ui.Rect{X: x, Y: y + 20, W: 160, H: 32}, &p.form[0])
		inputBox(f, ids[1], ui.Rect{X: x + 168, Y: y + 20, W: 128, H: 32}, &p.form[1])
		y += 64
		f.Text(ui.Rect{X: x, Y: y, W: r.W - 32, H: 16}, "搜索网址，用 {query} 标出搜索内容的位置", label)
		inputBox(f, ids[2], ui.Rect{X: x, Y: y + 20, W: r.W - 32, H: 32}, &p.form[2])
		y += 64
		save := ui.Button{Label: "保存", Kind: ui.ButtonPrimary, Small: true}
		cancel := ui.Button{Label: "取消", Kind: ui.ButtonGhost, Small: true}
		sw, cw := save.Width(), cancel.Width()
		right := r.Right() - 16
		if e := p.errs["form"]; e != "" {
			f.Text(ui.Rect{X: x, Y: y, W: right - sw - cw - 16 - x, H: 32}, e, ui.Text{Size: 12.5, Color: t.Destructive, Middle: true})
		}
		if f.Button("launcher.form.save", ui.Rect{X: right - sw, Y: y, W: sw, H: 32}, save) {
			p.saveForm(s, i)
		}
		if f.Button("launcher.form.cancel", ui.Rect{X: right - sw - 8 - cw, Y: y, W: cw, H: 32}, cancel) {
			p.editing = notEditing
		}
	}}
}

func (p *settingsPage) saveForm(s Settings, i int) {
	k := Keyword{Name: p.form[0].Text, Keyword: p.form[1].Text, URL: p.form[2].Text}
	next := slices.Clone(s.Keywords)
	from := ""
	if i >= 0 {
		from = next[i].Keyword
		next[i] = k
	} else {
		next = append(next, k)
	}
	if p.saveSites(s, next, from, strings.TrimSpace(k.Keyword), "form") {
		p.editing = notEditing
	}
}

// inputBox draws a one-line text box with its frame.
func inputBox(f *ui.Frame, id string, r ui.Rect, fld *ui.Field) {
	t := f.Theme
	border := t.Input
	if f.FocusedBox() == id {
		border = t.Ring
	}
	f.FillRound(r, design.Radius-2, t.Card)
	f.StrokeRound(r, design.Radius-2, 1, border)
	f.TextBox(id, r.InsetXY(10, 1), fld, t.Card)
}

// sync shows the saved value unless the user is typing in the box.
func (d *draft) sync(value string) {
	if d.focused {
		return
	}
	if !d.synced || d.base != value {
		d.field.Text, d.base, d.synced = value, value, true
	}
}

// box draws the draft's text box in r and calls commit with its text when
// it loses the focus, or on Enter in a one-line box. Whatever commit saves
// or refuses, the box then shows the saved value again.
func (d *draft) box(f *ui.Frame, id string, r ui.Rect, commit func(text string)) {
	enter := !d.field.Multiline && f.Event().ID == id && f.KeyPressed(ui.KeyReturn, 0)
	if d.field.Multiline {
		t := f.Theme
		border := t.Input
		if f.FocusedBox() == id {
			border = t.Ring
		}
		f.FillRound(r, design.Radius-2, t.Card)
		f.StrokeRound(r, design.Radius-2, 1, border)
		f.TextBox(id, r.InsetXY(10, 6), &d.field, t.Card)
	} else {
		inputBox(f, id, r, &d.field)
	}
	focused := f.FocusedBox() == id
	if enter {
		f.Blur()
		focused = false
	}
	if d.focused && !focused {
		d.focused = false
		commit(d.field.Text)
		d.synced = false
		f.Invalidate()
		return
	}
	d.focused = focused
}
