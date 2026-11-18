package core

import (
	"fmt"
	"math"
	"slices"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/ocr"
	"github.com/shirumesu/arubox/internal/ui"
)

// pollMs is how often the page asks for progress while a download runs.
const pollMs = 500

// keepChoices are how long PP-OCRv6 may stay loaded after use; 0 keeps it
// for good.
var keepChoices = []struct {
	minutes int
	label   string
}{{10, "10 分钟"}, {30, "30 分钟"}, {60, "1 小时"}, {240, "4 小时"}, {0, "永不释放"}}

// ocrPage sets up the OCR engine that screenshot translation and the
// screenshot module share. Asking the engine anything may wait for a read
// that is running, so every call goes off the UI thread.
type ocrPage struct {
	d  *dashboard
	st *ocr.Status // nil until loaded
	// loading is set while a status is being fetched, polling while a
	// fetch is scheduled.
	loading, polling bool
}

// Opened loads the status afresh.
func (p *ocrPage) Opened(*ui.Window) { p.reload() }

func (p *ocrPage) reload() {
	if p.loading {
		return
	}
	p.loading = true
	e := p.d.core.reader
	go func() {
		st := e.Status()
		ui.Post(func() {
			p.loading = false
			p.show(st)
		})
	}()
}

// run does action off the UI thread, then shows its error, if any, and
// the status after it.
func (p *ocrPage) run(action func(e *ocr.Engine) error) {
	e := p.d.core.reader
	go func() {
		err := action(e)
		st := e.Status()
		ui.Post(func() {
			p.show(st)
			p.d.fail(err)
		})
	}()
}

func (p *ocrPage) show(st ocr.Status) {
	p.st = &st
	p.d.invalidate()
	// Keep asking while a download runs, as long as the window is open.
	if st.Download != nil && st.Download.Running && !p.polling && p.d.win != nil {
		p.polling = true
		p.d.win.After(pollMs, func() {
			p.polling = false
			p.reload()
		})
	}
}

func (p *ocrPage) save(s ocr.Settings) {
	p.st.Settings = s
	p.run(func(e *ocr.Engine) error { return e.SetSettings(s) })
}

func (p *ocrPage) Draw(f *ui.Frame, x, y, w float64) float64 {
	st := p.st
	if st == nil {
		return y
	}
	engines := []string{ocr.EngineSystem, ocr.EnginePaddle}
	labels := []string{"系统（Windows OCR）", "PP-OCRv6"}
	y = f.Section(x, y, w, "识别引擎", []ui.Row{{
		Title: "识别引擎", Description: "截图翻译和截图里的“识别文字”都用它。下载 PP-OCRv6 后才能选它。",
		ControlW: 176,
		Control: func(r ui.Rect) {
			current := slices.Index(engines, st.Settings.Engine)
			label := ""
			if current >= 0 {
				label = labels[current]
			}
			items := make([]ui.MenuItem, len(engines))
			for i, e := range engines {
				items[i] = ui.MenuItem{
					Label: labels[i], Checked: i == current,
					Disabled: e == ocr.EnginePaddle && !st.Installed,
					Action: func() {
						if i != current {
							next := st.Settings
							next.Engine = e
							p.save(next)
						}
					},
				}
			}
			menuSelect(f, "ocr.engine", r, label, items)
		},
	}})
	y += sectionGap

	if !st.Supported {
		return f.Section(x, y, w, "PP-OCRv6", []ui.Row{{
			Title: "PP-OCRv6 small", Description: "这台电脑无法使用 PP-OCRv6。",
		}})
	}
	rows := []ui.Row{p.modelRow(f, st, w)}
	for _, file := range st.Files {
		size := megabytes(file.DownloadSize)
		sw, _ := ui.MeasureText(size, ui.Text{Size: 13}, 0)
		rows = append(rows, ui.Row{
			Title: file.Label, Description: file.License,
			ControlW: math.Ceil(sw) + 2,
			Control: func(r ui.Rect) {
				f.Text(r, size, ui.Text{Size: 13, Color: f.Theme.MutedForeground, Align: ui.AlignEnd, Middle: true})
			},
		})
	}
	rows = append(rows, p.keepRow(f, st))
	return f.Section(x, y, w, "PP-OCRv6", rows)
}

// modelRow is the model as a whole: what it reads, its download or
// deletion, and how the download goes.
func (p *ocrPage) modelRow(f *ui.Frame, st *ocr.Status, w float64) ui.Row {
	t := f.Theme
	var download, disk int64
	for _, file := range st.Files {
		download += file.DownloadSize
		disk += file.Size
	}
	progress := st.Download
	running := progress != nil && progress.Running
	row := ui.Row{
		Title: "PP-OCRv6 small",
		Description: fmt.Sprintf("百度飞桨的开源文字识别，离线可用，比系统识别更准。支持简体中文、繁体中文、英文、日文和其他拉丁字母语言，不支持韩文，不需要安装系统语言包。下载 %s，占用磁盘 %s，需要 Microsoft Visual C++ 运行库。",
			megabytes(download), megabytes(disk)),
	}
	if st.Installed {
		row.Badge, row.BadgeTone = "已下载", t.MutedForeground
	}
	var b ui.Button
	var action func(e *ocr.Engine) error
	switch {
	case running:
		b = ui.Button{Label: "取消", Icon: ui.IconClose, Small: true}
		action = func(e *ocr.Engine) error { e.CancelDownload(); return nil }
	case !st.Installed:
		b = ui.Button{Label: "下载", Icon: ui.IconDownload, Kind: ui.ButtonPrimary, Small: true}
		action = (*ocr.Engine).Download
	default:
		b = ui.Button{Label: "删除", Icon: ui.IconDelete, Kind: ui.ButtonGhost, Small: true}
		action = (*ocr.Engine).Delete
	}
	row.ControlW = b.Width()
	row.Control = func(r ui.Rect) {
		if f.Button("ocr.model", r, b) {
			p.run(action)
		}
	}

	// Below the text: the progress, the sources that failed and why the
	// download stopped.
	type line struct {
		text string
		col  design.Color
	}
	var lines []line
	if running {
		lines = append(lines, line{fmt.Sprintf("正在从 %s 下载%s，%s / %s", progress.Source, progress.File,
			megabytes(progress.Done), megabytes(progress.Total)), t.MutedForeground})
	}
	if progress != nil {
		for _, fl := range progress.Failures {
			lines = append(lines, line{fmt.Sprintf("%s：从 %s 下载失败（%s）", fl.File, fl.Source, fl.Error), t.MutedForeground})
		}
		if progress.Error != "" {
			lines = append(lines, line{progress.Error, t.Destructive})
		}
	}
	if len(lines) == 0 {
		return row
	}
	// The extra content spans the row inside its padding.
	textW := w - 32
	style := ui.Text{Size: 13, Wrap: true}
	heights := make([]float64, len(lines))
	h := 0.0
	if running {
		h += 6 + 6
	}
	for i, l := range lines {
		_, heights[i] = ui.MeasureText(l.text, style, textW)
		if i > 0 {
			h += 6
		}
		h += heights[i]
	}
	row.ExtraH = h + 12
	row.Extra = func(r ui.Rect) {
		y := r.Y + 8
		if running {
			frac := 0.0
			if progress.Total > 0 {
				frac = float64(progress.Done) / float64(progress.Total)
			}
			f.Progress(ui.Rect{X: r.X, Y: y, W: r.W, H: 6}, frac)
			y += 12
		}
		for i, l := range lines {
			s := style
			s.Color = l.col
			f.Text(ui.Rect{X: r.X, Y: y, W: r.W, H: heights[i]}, l.text, s)
			y += heights[i] + 6
		}
	}
	return row
}

func (p *ocrPage) keepRow(f *ui.Frame, st *ocr.Status) ui.Row {
	labels := make([]string, 0, len(keepChoices)+1)
	values := make([]int, 0, len(keepChoices)+1)
	for _, c := range keepChoices {
		labels, values = append(labels, c.label), append(values, c.minutes)
	}
	if !slices.Contains(values, st.Settings.KeepMinutes) {
		labels, values = append(labels, fmt.Sprintf("%d 分钟", st.Settings.KeepMinutes)), append(values, st.Settings.KeepMinutes)
	}
	const badge = "已加载"
	bw := 0.0
	if st.Loaded {
		w, _ := ui.MeasureText(badge, ui.Text{Size: 11.5, Weight: ui.Medium}, 0)
		bw = math.Ceil(w) + 14 + 6
	}
	return ui.Row{
		Title:       "用完后保持加载",
		Description: "识别过后保持加载这么久，期间再识别更快；之后释放它占用的内存（约 50–150 MB），下次识别时重新加载。",
		ControlW:    bw + 128,
		Control: func(r ui.Rect) {
			if st.Loaded {
				f.Badge(r.X, r.Y+6, badge, f.Theme.MutedForeground)
			}
			i := slices.Index(values, st.Settings.KeepMinutes)
			if f.Select("ocr.keep", ui.Rect{X: r.X + bw, Y: r.Y, W: 128, H: r.H}, labels, &i) {
				next := st.Settings
				next.KeepMinutes = values[i]
				p.save(next)
			}
		},
	}
}

// menuSelect draws a drop-down like Frame.Select that opens a menu of
// items, some of which may be disabled.
func menuSelect(f *ui.Frame, id string, r ui.Rect, label string, items []ui.MenuItem) {
	t := f.Theme
	res := f.Interact(id, r)
	f.SetCursor(r, ui.CursorHand)
	bg := t.Card
	if res.Hovered {
		bg = t.Accent
	}
	f.FillRound(r, design.Radius-3, bg)
	f.StrokeRound(r, design.Radius-3, 1, t.Input)
	f.Text(ui.Rect{X: r.X + 10, Y: r.Y, W: r.W - 34, H: r.H}, label, ui.Text{Size: 13, Color: t.Foreground, Middle: true})
	f.Icon(ui.Rect{X: r.Right() - 26, Y: r.Y, W: 18, H: r.H}, ui.IconChevronDown, 10, t.MutedForeground)
	if res.Clicked {
		f.Menu(r, items)
	}
}

// megabytes shows bytes as megabytes, the way the model sites state them.
func megabytes(bytes int64) string { return fmt.Sprintf("%.1f MB", float64(bytes)/1e6) }
