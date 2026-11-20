package launcher

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/png"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
)

// The panel's layout, in DIPs.
const (
	panelWidth = 640.0
	// panelTop places the panel's top this far down the work area, as a
	// fraction of its height, like Spotlight.
	panelTop = 0.22
	headerH  = 56.0
	// listMaxH is the most the result list grows to before it scrolls.
	listMaxH = 396.0
	listPad  = 6.0
	rowMinH  = 44.0
	rowIcon  = 28.0
	footerH  = 32.0
	emptyH   = 44.0
	// titleLine is the line height of a result's title; a translation
	// shows up to three lines.
	titleLine     = 20.0
	subtitleLine  = 16.0
	maxTitleLines = 3
)

const (
	// moreDelay waits this long after typing stops before the slower
	// search for files and rates.
	moreDelay = 120 * time.Millisecond
	// translateDelay translates only once typing has paused, so a sentence
	// is not sent word by word.
	translateDelay = 450 * time.Millisecond
	// maxImages bounds the decoded icons the panel holds; they are freed
	// whenever it hides.
	maxImages = 128
)

const (
	queryBox = "query"
	listID   = "list"
)

// Segoe Fluent glyphs the ui package has no name for.
const (
	glyphTag      = ''
	glyphTerminal = ''
)

// chip is a keyword in use, shown as a tag before the query: a website to
// search, or translating or searching files whatever the query looks like.
type chip struct {
	kind string // "web", "translate" or "files"
	word string
	name string
}

var chipPlaceholders = map[string]string{
	"web":       "输入要搜索的内容",
	"translate": "输入要翻译的文字",
	"files":     "输入文件或文件夹的名称",
}

var chipGlyphs = map[string]rune{"web": ui.IconGlobe, "translate": ui.IconTranslate, "files": ui.IconFolder}

const placeholder = "搜索应用和文件，输入算式，或用 ! 开头运行命令"

// keywords lists every keyword the settings define, website ones last.
func keywords(s Settings) []chip {
	var all []chip
	if s.TranslateKeyword != "" {
		all = append(all, chip{"translate", s.TranslateKeyword, "翻译"})
	}
	if s.FilesKeyword != "" {
		all = append(all, chip{"files", s.FilesKeyword, "搜索文件"})
	}
	for _, k := range s.Keywords {
		all = append(all, chip{"web", k.Keyword, k.Name})
	}
	return all
}

func findChip(s Settings, word string) *chip {
	if word == "" {
		return nil
	}
	for _, c := range keywords(s) {
		if strings.EqualFold(c.word, word) {
			return &c
		}
	}
	return nil
}

// webRow searches a website for query, or opens it when query is empty.
func webRow(c chip, query string) Result {
	q := strings.TrimSpace(query)
	sub := "用 " + c.name + " 搜索"
	if q == "" {
		sub = "打开 " + c.name
	}
	return Result{Kind: kindWeb, Key: c.word, Title: q, Subtitle: sub}
}

// suggestions offer the keywords that start with what is typed, to take
// with Tab.
func suggestions(s Settings, query string) []Result {
	q := strings.ToLower(strings.TrimSpace(query))
	if utf8.RuneCountInString(q) < 2 || strings.ContainsFunc(q, unicode.IsSpace) {
		return nil
	}
	var out []Result
	for _, c := range keywords(s) {
		if strings.HasPrefix(strings.ToLower(c.word), q) && len(out) < 3 {
			out = append(out, Result{Kind: kindKeyword, Key: c.word, Title: c.name, Subtitle: "关键词 " + c.word})
		}
	}
	return out
}

// cutKeyword splits "word rest" at its first space; ok is false when the
// text does not start with a word followed by a space.
func cutKeyword(text string) (word, rest string, ok bool) {
	i := strings.IndexFunc(text, unicode.IsSpace)
	if i <= 0 {
		return "", "", false
	}
	_, size := utf8.DecodeRuneInString(text[i:])
	return text[:i], text[i+size:], true
}

// copyable reports whether a result is a value to copy: a calculation, a
// conversion, a time or a translation.
func copyable(r Result) bool {
	switch r.Kind {
	case kindCalc, kindCurrency, kindTime, kindTranslation:
		return r.Key != ""
	}
	return false
}

// copyText is what copying a result copies: a translation's translated
// text, the others' value.
func copyText(r Result) string {
	if r.Kind == kindTranslation {
		return r.Title
	}
	return r.Key
}

func actionName(r Result) string {
	if copyable(r) {
		return "复制并关闭"
	}
	switch r.Kind {
	case kindCommand:
		return "运行"
	case kindWeb:
		if r.Title != "" {
			return "在浏览器中搜索"
		}
		return "在浏览器中打开"
	case kindKeyword:
		return "使用"
	}
	return "打开"
}

func kindGlyph(kind string) rune {
	switch kind {
	case kindCalc:
		return ui.IconCalculator
	case kindCurrency:
		return ui.IconMoney
	case kindTime:
		return ui.IconClock
	case kindTranslation:
		return ui.IconTranslate
	case kindWeb:
		return ui.IconGlobe
	case kindKeyword:
		return glyphTag
	}
	return glyphTerminal
}

func isTranslation(r Result) bool { return r.Kind == kindTranslation && r.Key != "" }

// place centres the panel horizontally in the work area of m with its top
// a fixed way down, keeping it inside the area however tall it grows.
// height is in DIPs; the result is in physical pixels.
func place(height float64, m ui.Monitor) ui.Bounds {
	s := m.Scale
	if s <= 0 {
		s = 1
	}
	w := int(math.Round(panelWidth * s))
	h := min(int(math.Ceil(height*s)), m.Work.H)
	x := m.Work.X + (m.Work.W-w)/2
	y := m.Work.Y + int(float64(m.Work.H)*panelTop)
	y = min(y, m.Work.Y+m.Work.H-h)
	return ui.Bounds{X: x, Y: y, W: w, H: h}
}

// decodeIcon turns a PNG into the premultiplied BGRA pixels the canvas
// draws, or nil.
func decodeIcon(data []byte) *ui.Image {
	if len(data) == 0 {
		return nil
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	// image.RGBA is premultiplied, so drawing into it premultiplies.
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	px := dst.Pix
	for i := 0; i+3 < len(px); i += 4 {
		px[i], px[i+2] = px[i+2], px[i]
	}
	return ui.NewImage(b.Dx(), b.Dy(), px)
}

// panel is the launcher window. Its window is made on the first open and
// hidden rather than closed, so later opens show at once and find the last
// query; a hidden native window costs little. Everything in it belongs to
// the UI thread.
type panel struct {
	m   *Module
	win *ui.Window
	// monitor is the display the panel opened on.
	monitor ui.Monitor

	field       ui.Field
	chip        *chip
	instant     []Result
	more        []Result
	translation []Result
	selected    int
	// chosen is set once the user has moved the selection: results that
	// arrive later then keep the same result selected, not the same row.
	chosen bool
	// copied shows that a value was copied, until the next key.
	copied bool

	// seq numbers searches; only the latest one's results may show.
	seq int
	// stop cancels the last search's delayed parts.
	stop func()

	resetScroll bool
	scrollToSel bool
	fitQueued   bool

	images   map[string]*ui.Image // decoded icons by cache key
	fetching map[string]bool
}

func newPanel(m *Module) *panel {
	return &panel{m: m, field: ui.Field{Size: 18}, images: map[string]*ui.Image{}, fetching: map[string]bool{}}
}

// toggle shows the panel on the display under the mouse, or hides it if
// it is showing.
func (p *panel) toggle() {
	if p.win != nil && p.win.Visible() {
		p.hide()
		return
	}
	x, y := ui.CursorPos()
	p.monitor = ui.MonitorAt(x, y)
	if p.win == nil {
		w, err := ui.NewWindow(ui.WindowOptions{Kind: ui.Popup, Title: "启动器", Topmost: true, Bounds: place(headerH, p.monitor)}, p)
		if err != nil {
			p.m.log.Error("create launcher panel", "error", err)
			return
		}
		w.OnDeactivate = func() {
			// After the activation change is done, and only if the panel
			// did not get the focus back meanwhile.
			ui.Post(func() {
				if p.win == w && !w.Active() {
					p.hide()
				}
			})
		}
		p.win = w
	}
	// Select the last query, so typing replaces it, and refresh its
	// results: the apps or the keywords may have changed.
	p.field.Focus, p.field.SelectAll = true, true
	// The index loads while the panel opens and the user types, and stays
	// until the panel has been closed for a while.
	if err := p.m.useFiles(); err != nil {
		p.m.log.Error("start file index", "error", err)
	}
	p.m.stopIdle()
	p.search(p.field.Text, p.chip)
	p.fit()
	p.win.Show(true)
}

func (p *panel) hide() {
	p.cancelSearch()
	if p.win != nil {
		p.win.Hide()
	}
	p.releaseImages()
	p.m.idleFiles()
}

// destroy closes the window, when the module is disabled.
func (p *panel) destroy() {
	p.cancelSearch()
	p.releaseImages()
	if p.win != nil {
		p.win.Close()
		p.win = nil
	}
}

func (p *panel) cancelSearch() {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

func (p *panel) releaseImages() {
	for _, img := range p.images {
		img.Release()
	}
	clear(p.images)
}

// fit sizes the panel to its content, growing downwards from a fixed top.
func (p *panel) fit() {
	if p.win == nil {
		return
	}
	b := place(p.height(p.results()), p.monitor)
	before := p.win.Scale()
	p.win.SetBounds(b)
	if p.win.Scale() != before {
		// Moving to a display of another scale resized the window to keep
		// its DIP size; the bounds are already right for the new scale.
		p.win.SetBounds(b)
	}
}

// search runs a query: the instant results at once, the slower ones once
// typing pauses. A newer query cancels the older one's.
func (p *panel) search(q string, c *chip) {
	p.seq++
	n := p.seq
	p.cancelSearch()
	ctx, cancel := context.WithCancel(context.Background())
	var timers []*time.Timer
	p.stop = func() {
		cancel()
		for _, t := range timers {
			t.Stop()
		}
	}
	later := func(delay time.Duration, fetch func(context.Context, string) []Result, set func([]Result)) {
		timers = append(timers, time.AfterFunc(delay, func() {
			r := fetch(ctx, q)
			if ctx.Err() != nil {
				return
			}
			ui.Post(func() {
				if n != p.seq || p.win == nil {
					return
				}
				// Results come in above others, which would put another
				// one under a selection the user made.
				var keep *Result
				if p.chosen {
					keep = p.current(p.results())
				}
				set(r)
				if keep != nil {
					if i := slices.Index(p.results(), *keep); i >= 0 && i != p.selected {
						p.selected, p.scrollToSel = i, true
					}
				}
				p.win.Invalidate()
			})
		}))
	}
	setMore := func(r []Result) { p.more = r }
	setTranslation := func(r []Result) { p.translation = r }
	m := p.m

	p.selected, p.chosen, p.resetScroll = 0, false, true
	p.more, p.translation = nil, nil
	if c != nil {
		p.instant = nil
		if c.kind == "web" {
			p.instant = []Result{webRow(*c, q)}
		}
		if strings.TrimSpace(q) != "" {
			switch c.kind {
			case "translate":
				later(translateDelay, func(ctx context.Context, q string) []Result { return m.translate(ctx, q, true) }, setTranslation)
			case "files":
				later(moreDelay, func(ctx context.Context, q string) []Result { return m.searchFiles(ctx, q, maxFiles) }, setMore)
			}
		}
		return
	}
	p.instant = m.search(q)
	later(moreDelay, m.searchMore, setMore)
	later(translateDelay, func(ctx context.Context, q string) []Result { return m.translate(ctx, q, false) }, setTranslation)
}

func (p *panel) applyChip(c *chip, rest string) {
	p.chip = c
	p.field.Text = rest
	p.search(rest, c)
}

// onChange follows the typed text. A keyword and a space turn into the
// keyword's tag.
func (p *panel) onChange(text string) {
	if p.chip == nil {
		if word, rest, ok := cutKeyword(text); ok {
			if c := findChip(p.m.currentSettings(), word); c != nil {
				p.applyChip(c, rest)
				return
			}
		}
	}
	p.search(text, p.chip)
}

// results are the rows in order: the instant ones, the keywords offered,
// the translation, the slower ones and the default search.
func (p *panel) results() []Result {
	s := p.m.currentSettings()
	q := strings.TrimSpace(p.field.Text)
	out := append([]Result(nil), p.instant...)
	if p.chip == nil {
		out = append(out, suggestions(s, p.field.Text)...)
	}
	out = append(out, p.translation...)
	out = append(out, p.more...)
	if p.chip == nil && q != "" && !strings.HasPrefix(q, "!") {
		if c := findChip(s, s.DefaultSearch); c != nil && c.kind == "web" {
			out = append(out, webRow(*c, q))
		}
	}
	return out
}

func (p *panel) showList() bool {
	return strings.TrimSpace(p.field.Text) != "" || (p.chip != nil && p.chip.kind == "web")
}

// titleWidth is the width a row's title has.
const titleWidth = panelWidth - 2*listPad - 10 - rowIcon - 12 - 10

var translationText = ui.Text{Size: 14, Wrap: true, LineHeight: titleLine}

func titleHeight(r Result) float64 {
	if !isTranslation(r) {
		return titleLine
	}
	_, h := ui.MeasureText(r.Title, translationText, titleWidth)
	return min(max(h, titleLine), maxTitleLines*titleLine)
}

func rowHeight(r Result) float64 {
	return max(rowMinH, titleHeight(r)+subtitleLine+12)
}

// height is how tall the panel's content is, in DIPs.
func (p *panel) height(rs []Result) float64 {
	h := headerH
	if !p.showList() {
		return h
	}
	h++
	if len(rs) == 0 {
		return h + emptyH
	}
	content := 2 * listPad
	for _, r := range rs {
		content += rowHeight(r)
	}
	h += min(content, listMaxH)
	if p.selected < len(rs) {
		h += 1 + footerH
	}
	return h
}

func (p *panel) clampSelection(rs []Result) {
	p.selected = max(0, min(p.selected, len(rs)-1))
}

func (p *panel) current(rs []Result) *Result {
	if p.selected >= 0 && p.selected < len(rs) {
		return &rs[p.selected]
	}
	return nil
}

func (p *panel) move(rs []Result, d int) {
	if len(rs) > 0 {
		p.selected = (p.selected + d + len(rs)) % len(rs)
		p.chosen, p.scrollToSel = true, true
	}
}

// activate acts on a result. An offered keyword becomes the tag; anything
// else hides the panel first.
func (p *panel) activate(r *Result, reveal bool) {
	if r == nil {
		return
	}
	if r.Kind == kindKeyword {
		if c := findChip(p.m.currentSettings(), r.Key); c != nil {
			p.applyChip(c, "")
		}
		p.field.Focus = true
		return
	}
	p.hide()
	res := *r
	go func() {
		if err := p.m.activate(res, reveal); err != nil {
			p.m.log.Error("launcher activate", "kind", res.Kind, "error", err)
		}
	}()
}

// key handles the keys typed into the search box, or into the panel while
// the box lost the focus.
func (p *panel) key(f *ui.Frame, rs []Result) {
	ev := f.Event()
	cur := p.current(rs)
	p.copied = false
	ctrl := ev.Mods == ui.ModifierCtrl
	switch {
	case ev.Key == 'C' && ctrl && cur != nil && copyable(*cur):
		// Ctrl+C copies the value and keeps the panel open, so it can be
		// pasted back to calculate on. With text selected in the box it
		// copies that text as usual.
		if s, e, ok := f.TextSelection(queryBox); ok && s != e {
			return
		}
		f.TakeKey()
		if err := platform.CopyText(copyText(*cur)); err != nil {
			p.m.log.Warn("launcher copy", "error", err)
			return
		}
		p.copied = true
	case f.KeyPressed(ui.KeyTab, 0):
		// Tab takes the keyword offered, or the keyword typed.
		if cur != nil && cur.Kind == kindKeyword {
			p.activate(cur, false)
		} else if p.chip == nil {
			if c := findChip(p.m.currentSettings(), strings.TrimSpace(p.field.Text)); c != nil {
				p.applyChip(c, "")
			}
		}
	case ev.Key == ui.KeyBack && ev.Mods == 0 && p.chip != nil && p.field.Text == "":
		// Deleting before the query gives the keyword back as text.
		f.TakeKey()
		p.applyChip(nil, p.chip.word)
	case f.KeyPressed(ui.KeyDown, 0) || f.KeyPressed('N', ui.ModifierCtrl):
		p.move(rs, 1)
	case f.KeyPressed(ui.KeyUp, 0) || f.KeyPressed('P', ui.ModifierCtrl):
		p.move(rs, -1)
	case f.KeyPressed(ui.KeyReturn, 0):
		p.activate(cur, false)
	case f.KeyPressed(ui.KeyReturn, ui.ModifierCtrl):
		p.activate(cur, true)
	case f.KeyPressed(ui.KeyEscape, 0):
		switch {
		case p.field.Text != "":
			p.field.Text = ""
			p.search("", p.chip)
		case p.chip != nil:
			p.applyChip(nil, "")
		default:
			p.hide()
		}
	}
}

// Frame draws the panel and handles its input.
func (p *panel) Frame(f *ui.Frame) {
	t := f.Theme
	width, _ := f.Size()
	ev := f.Event()
	rs := p.results()
	p.clampSelection(rs)

	if ev.Kind == ui.EvTextChanged {
		// Typed characters reach the box without a key frame.
		p.copied = false
	}
	if ev.Kind == ui.EvKeyDown && (ev.ID == queryBox || ev.ID == "") {
		p.key(f, rs)
		rs = p.results()
		p.clampSelection(rs)
	}
	if ev.Kind == ui.EvMouseUp && p.win.Visible() && f.FocusedBox() != queryBox {
		// A click anywhere but the box leaves the typing in it.
		p.field.Focus = true
		f.Invalidate()
	}

	// The search box.
	f.Icon(ui.Rect{X: 16, Y: (headerH - 20) / 2, W: 20, H: 20}, ui.IconSearch, 17, t.MutedForeground)
	x := 16.0 + 20 + 12
	p.field.Placeholder = placeholder
	if p.chip != nil {
		x += p.drawChip(f, x, *p.chip) + 12
		p.field.Placeholder = chipPlaceholders[p.chip.kind]
	}
	if f.TextBox(queryBox, ui.Rect{X: x, Y: 0, W: width - 16 - x, H: headerH}, &p.field, t.Background) {
		p.field.Changed = false
		p.onChange(p.field.Text)
		rs = p.results()
		p.clampSelection(rs)
		f.Invalidate()
	}

	if p.showList() {
		y := headerH
		f.Fill(ui.Rect{X: 0, Y: y, W: width, H: 1}, t.Border)
		y++
		if len(rs) == 0 {
			f.Text(ui.Rect{X: 16, Y: y, W: width - 32, H: emptyH}, "没有结果", ui.Text{Size: 13, Color: t.MutedForeground, Middle: true})
		} else {
			y += p.drawList(f, y, width, rs)
			if cur := p.current(rs); cur != nil {
				p.drawFooter(f, y, width, *cur)
			}
		}
	}
	p.checkHeight(rs)
}

// checkHeight refits the window once this frame is done when the content
// no longer matches its size.
func (p *panel) checkHeight(rs []Result) {
	if p.fitQueued || !p.win.Visible() {
		return
	}
	want := place(p.height(rs), p.monitor)
	if have := p.win.Bounds(); have.H == want.H && have.Y == want.Y {
		return
	}
	p.fitQueued = true
	ui.Post(func() {
		p.fitQueued = false
		if p.win != nil && p.win.Visible() {
			p.fit()
		}
	})
}

// drawChip draws the keyword's tag at x and returns its width.
func (p *panel) drawChip(f *ui.Frame, x float64, c chip) float64 {
	t := f.Theme
	style := ui.Text{Size: 13, Weight: ui.Medium, Color: t.Primary, Middle: true}
	tw, _ := ui.MeasureText(c.name, style, 0)
	r := ui.Rect{X: x, Y: (headerH - 26) / 2, W: math.Ceil(8 + 14 + 4 + tw + 8), H: 26}
	f.FillRound(r, design.Radius-2, t.Primary.WithAlpha(0.1))
	f.Icon(ui.Rect{X: r.X + 8, Y: r.Y, W: 14, H: r.H}, chipGlyphs[c.kind], 13, t.Primary)
	f.Text(ui.Rect{X: r.X + 26, Y: r.Y, W: tw + 2, H: r.H}, c.name, style)
	return r.W
}

// drawList draws the results from y and returns the list's height.
func (p *panel) drawList(f *ui.Frame, y, width float64, rs []Result) float64 {
	heights := make([]float64, len(rs))
	content := 2 * listPad
	for i, r := range rs {
		heights[i] = rowHeight(r)
		content += heights[i]
	}
	view := ui.Rect{X: 0, Y: y, W: width, H: min(content, listMaxH)}
	if p.resetScroll {
		p.resetScroll = false
		f.ResetScroll(listID)
	}
	if p.scrollToSel {
		// Keep the selected row in view as the arrow keys move it.
		p.scrollToSel = false
		top := listPad
		for _, h := range heights[:p.selected] {
			top += h
		}
		bottom := top + heights[p.selected]
		if p.selected == 0 {
			top = 0
		}
		if p.selected == len(rs)-1 {
			bottom += listPad
		}
		f.ScrollTo(listID, top, bottom, view.H)
	}
	f.Scroll(listID, view, content, func(area ui.Rect) {
		ry := area.Y + listPad
		for i, r := range rs {
			p.drawRow(f, i, ui.Rect{X: area.X + listPad, Y: ry, W: area.W - 2*listPad, H: heights[i]}, r)
			ry += heights[i]
		}
	})
	return view.H
}

func (p *panel) drawRow(f *ui.Frame, i int, r ui.Rect, res Result) {
	t := f.Theme
	ev := f.Event()
	// The row is named by its result, not its place: results that come in
	// between the press and the release move the others down, and the
	// click must not open what took the pressed one's place.
	resp := f.Interact("row\x00"+res.Kind+"\x00"+res.Key+"\x00"+res.Title, r)
	if resp.Hovered && ev.Kind == ui.EvMouseMove && p.selected != i {
		p.selected, p.chosen = i, true
		f.Invalidate()
	}
	if resp.Clicked {
		p.activate(&res, ev.Mods&ui.ModifierCtrl != 0)
	}
	fg := t.Foreground
	if i == p.selected {
		f.FillRound(r, design.Radius-2, t.Accent)
		fg = t.AccentForeground
	}
	p.drawIcon(f, ui.Rect{X: r.X + 10, Y: r.Y + (r.H-rowIcon)/2, W: rowIcon, H: rowIcon}, res)

	tx := r.X + 10 + rowIcon + 12
	tw := r.Right() - 10 - tx
	th := titleHeight(res)
	ty := r.Y + (r.H-th-subtitleLine)/2
	// A website opened without a query says so in its title.
	home := res.Kind == kindWeb && res.Title == ""
	title, subtitle := res.Title, res.Subtitle
	switch {
	case home:
		title, subtitle = res.Subtitle, "输入内容后按 ↵ 搜索"
	case isTranslation(res):
		subtitle = languageName(res.Source) + " → " + languageName(res.Target) + " · " + res.Subtitle
	}
	if isTranslation(res) {
		style := translationText
		style.Color = fg
		f.Text(ui.Rect{X: tx, Y: ty, W: tw, H: th}, title, style)
	} else {
		f.Text(ui.Rect{X: tx, Y: ty, W: tw, H: titleLine}, title, ui.Text{Size: 14, Color: fg, Middle: true})
	}
	f.Text(ui.Rect{X: tx, Y: ty + th, W: tw, H: subtitleLine}, subtitle, ui.Text{Size: 11, Color: t.MutedForeground, Middle: true})
}

// drawIcon draws a result's shell icon, or the glyph of its kind. A shell
// icon still being drawn leaves the place empty for a moment.
func (p *panel) drawIcon(f *ui.Frame, r ui.Rect, res Result) {
	if res.Icon != "" {
		img, pending := p.image(res)
		if img != nil {
			f.Image(img, r, ui.Rect{}, 1, true)
			return
		}
		if pending {
			return
		}
	}
	t := f.Theme
	f.FillRound(r, design.Radius-4, t.Muted)
	f.Icon(r, kindGlyph(res.Kind), 15, t.MutedForeground)
}

// image returns the decoded icon of a result. pending is true while the
// module is still drawing it; the panel repaints when it is done.
func (p *panel) image(res Result) (img *ui.Image, pending bool) {
	key := res.Icon
	if img, ok := p.images[key]; ok {
		return img, false
	}
	data, ok := p.m.cachedIcon(key)
	if !ok {
		if !p.fetching[key] {
			p.fetching[key] = true
			target := res.Key
			go func() {
				p.m.icon(key, target)
				ui.Post(func() {
					delete(p.fetching, key)
					if p.win != nil {
						p.win.Invalidate()
					}
				})
			}()
		}
		return nil, true
	}
	if len(p.images) >= maxImages {
		p.releaseImages()
	}
	img = decodeIcon(data)
	p.images[key] = img
	return img, false
}

// drawFooter shows the keys that act on the selected result.
func (p *panel) drawFooter(f *ui.Frame, y, width float64, cur Result) {
	t := f.Theme
	f.Fill(ui.Rect{X: 0, Y: y, W: width, H: 1}, t.Border)
	y++
	hints := []string{"↑↓ 选择"}
	if copyable(cur) {
		if p.copied {
			hints = append(hints, "已复制")
		} else {
			hints = append(hints, "Ctrl+C 复制")
		}
	}
	hints = append(hints, "↵ "+actionName(cur))
	switch {
	case cur.Kind == kindFile || cur.Kind == kindFolder:
		hints = append(hints, "Ctrl+↵ 在资源管理器中显示")
	case isTranslation(cur):
		hints = append(hints, "Ctrl+↵ 在翻译窗口中打开")
	case cur.Kind == kindKeyword:
		hints = append(hints, "Tab 使用")
	}
	style := ui.Text{Size: 11, Color: t.MutedForeground, Middle: true}
	x := 16.0
	for _, h := range hints {
		w, _ := ui.MeasureText(h, style, 0)
		f.Text(ui.Rect{X: x, Y: y, W: w + 1, H: footerH}, h, style)
		x += w + 16
	}
	esc := "Esc 关闭"
	switch {
	case p.field.Text != "":
		esc = "Esc 清空"
	case p.chip != nil:
		esc = "Esc 取消关键词"
	}
	style.Align = ui.AlignEnd
	f.Text(ui.Rect{X: x, Y: y, W: width - 16 - x, H: footerH}, esc, style)
}
