package translate

import (
	"context"
	"math"
	"strings"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
)

const (
	popupWidth = 420.0
	// popupGap keeps the popup this far from the cursor and the screen
	// edges, in DIPs.
	popupGap = 12.0
	// maxSourceHeight is how tall the source text grows before it scrolls,
	// and maxResultHeight the same for the translation.
	maxSourceHeight = 168.0
	maxResultHeight = 320.0
	// The text boxes use the font the native edit control is given, so
	// measuring with it predicts their line breaks.
	boxFont = "Microsoft YaHei UI"
	boxSize = 15.0
)

// request is what the popup shows when it opens.
type request struct {
	// text is translated right away; empty opens the popup for typing.
	text string
	// notice explains why text is empty when a selection was expected.
	notice string
}

type resultKind int

const (
	resultEmpty resultKind = iota
	resultLoading
	resultDone
	resultError
)

// popup is the floating translation window. Its state lives here, on the
// UI thread; the window itself is made when it opens and closed when it
// hides, so a hidden popup holds no drawing surface.
type popup struct {
	m   *Module
	win *ui.Window

	pinned bool
	notice string
	source ui.Field
	// output shows the translation in a read-only box, so part of it can
	// be selected and copied.
	output ui.Field

	result      resultKind
	translation Translation
	errText     string
	cancel      context.CancelFunc
	// seq numbers translations; only the latest may show.
	seq int

	// speaking is "source" or "result" while that text is read aloud.
	speaking   string
	stopSpeech func()
	speechSeq  int
	speechErr  string

	copied  bool
	copySeq int

	fitting  bool // a resize to the content is posted
	spinning bool // a frame to turn the spinner is scheduled
}

// open shows req next to the mouse. A pinned popup that is shown stays
// where the user put it.
func (p *popup) open(req request) {
	p.stopReading()
	p.speechErr = ""
	p.notice = req.notice
	p.source = ui.Field{Text: crlf(req.text), Multiline: true, Size: boxSize, Focus: true}
	p.output = ui.Field{Multiline: true, ReadOnly: true, Size: boxSize}
	p.translate(req.text)

	cx, cy := ui.CursorPos()
	mon := ui.MonitorAt(cx, cy)
	switch {
	case p.win == nil:
		p.pinned = false
		w, err := ui.NewWindow(ui.WindowOptions{
			Kind:    ui.Popup,
			Title:   "翻译",
			Topmost: true,
			Bounds:  place(cx, cy, p.height(), mon),
		}, p)
		if err != nil {
			p.m.log.Warn("open translation popup", "error", err)
			return
		}
		w.OnDeactivate = func() {
			if p.win == w && !p.pinned {
				p.hide()
			}
		}
		w.OnClose = func() bool {
			if p.win == w {
				p.hide()
			}
			return false
		}
		p.win = w
	case !p.pinned:
		p.win.SetBounds(place(cx, cy, p.height(), mon))
	default:
		p.fit()
	}
	p.win.Show(true)
}

// hide closes the popup and stops what it was doing.
func (p *popup) hide() {
	p.stopReading()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.seq++
	w := p.win
	if w == nil {
		return
	}
	p.win = nil
	p.pinned = false
	w.Hide()
	// Not closed at once: this may run inside the window's own frame,
	// which still draws after the view returns.
	ui.Post(w.Close)
}

// translate starts translating text, replacing any translation under way.
func (p *popup) translate(text string) {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.seq++
	text = strings.TrimSpace(text)
	if text == "" {
		p.result = resultEmpty
		return
	}
	p.result = resultLoading
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	seq := p.seq
	go func() {
		t, err := p.m.translate(ctx, text)
		ui.Post(func() {
			cancel()
			if p.seq != seq {
				return
			}
			p.cancel = nil
			if err != nil {
				p.result, p.errText = resultError, err.Error()
			} else {
				p.result, p.translation = resultDone, t
				p.output.Text = crlf(t.Text)
			}
			if p.win != nil {
				p.win.Invalidate()
			}
		})
	}()
}

// changeSettings saves a change made in the popup and translates text
// again with it.
func (p *popup) changeSettings(change func(*Settings), text string) {
	s := p.m.currentSettings()
	change(&s)
	if err := p.m.setSettings(s); err != nil {
		p.result, p.errText = resultError, err.Error()
		return
	}
	p.translate(text)
}

// toggleSpeech reads text aloud, or stops it when the same button started
// the reading.
func (p *popup) toggleSpeech(key, text, lang string) {
	was := p.speaking
	p.stopReading()
	p.speechErr = ""
	if was == key {
		return
	}
	p.speaking = key
	p.speechSeq++
	seq := p.speechSeq
	p.stopSpeech = p.m.readAloud(text, lang, p.m.currentSettings().Voice, func(err error) {
		if p.speechSeq != seq {
			return
		}
		p.speaking, p.stopSpeech = "", nil
		if err != nil {
			p.speechErr = err.Error()
		}
		if p.win != nil {
			p.win.Invalidate()
		}
	})
}

func (p *popup) stopReading() {
	p.speechSeq++
	if p.stopSpeech != nil {
		p.stopSpeech()
		p.stopSpeech = nil
	}
	p.speaking = ""
}

func (p *popup) copyResult() {
	if err := platform.CopyText(p.translation.Text); err != nil {
		p.speechErr = err.Error()
		return
	}
	p.copied = true
	p.copySeq++
	seq, w := p.copySeq, p.win
	w.After(1200, func() {
		if p.copySeq == seq {
			p.copied = false
			w.Invalidate()
		}
	})
}

// swapLanguage is the language the source swaps to: the one asked for, or
// the one detected. It is "" before a language was detected.
func (p *popup) swapLanguage(s Settings) string {
	if s.Source != autoDetect {
		return s.Source
	}
	if p.result == resultDone {
		return p.translation.Source
	}
	return ""
}

// swap turns the translation into the text to translate back.
func (p *popup) swap(s Settings) {
	to := p.swapLanguage(s)
	if to == "" {
		return
	}
	text, from := p.source.Text, s.Target
	if p.result == resultDone {
		text, from = crlf(p.translation.Text), p.translation.Target
	}
	p.source.Text = text
	p.changeSettings(func(s *Settings) { s.Source, s.Target = from, to }, text)
}

// layout is where the parts of the popup go, in DIPs.
type layout struct {
	notice                    ui.Rect
	sourceCard, sourceBox     ui.Rect
	sourceBar                 ui.Rect
	swap                      ui.Rect
	resultCard, resultBar     ui.Rect
	resultBody, resultActions ui.Rect
	speechErr                 ui.Rect
	height                    float64
}

// layout measures the content for the current state.
func (p *popup) layout() layout {
	var l layout
	x, w := 10.0, popupWidth-20
	y := 40.0
	if p.notice != "" {
		_, h := ui.MeasureText(p.notice, ui.Text{Size: 13, Wrap: true}, w-24)
		l.notice = ui.Rect{X: x, Y: y, W: w, H: math.Ceil(h) + 16}
		y = l.notice.Bottom() + 8
	}
	sh := boxHeight(p.source.Text, w-24, maxSourceHeight)
	l.sourceCard = ui.Rect{X: x, Y: y, W: w, H: 10 + sh + 36}
	l.sourceBox = ui.Rect{X: x + 12, Y: y + 10, W: w - 24, H: sh}
	l.sourceBar = ui.Rect{X: x + 6, Y: l.sourceBox.Bottom(), W: w - 12, H: 36}
	y = l.sourceCard.Bottom()
	l.swap = ui.Rect{X: popupWidth/2 - 14, Y: y + 4 - 14, W: 28, H: 28}
	y += 8

	top := y
	l.resultBar = ui.Rect{X: x + 6, Y: y, W: w - 12, H: 36}
	y += 36
	switch p.result {
	case resultError:
		_, h := ui.MeasureText(p.errText, ui.Text{Size: 13, Wrap: true}, w-24)
		l.resultBody = ui.Rect{X: x + 12, Y: y, W: w - 24, H: math.Ceil(h)}
		y += l.resultBody.H + 12
	case resultDone:
		h := boxHeight(p.output.Text, w-24, maxResultHeight)
		l.resultBody = ui.Rect{X: x + 12, Y: y, W: w - 24, H: h}
		y += h
		l.resultActions = ui.Rect{X: x + 6, Y: y, W: w - 12, H: 32}
		y += 32
	default:
		l.resultBody = ui.Rect{X: x + 12, Y: y, W: w - 24, H: 24}
		y += 24 + 12
	}
	l.resultCard = ui.Rect{X: x, Y: top, W: w, H: y - top}
	if p.speechErr != "" {
		_, h := ui.MeasureText(p.speechErr, ui.Text{Size: 13, Wrap: true}, popupWidth-24)
		l.speechErr = ui.Rect{X: 12, Y: y + 8, W: popupWidth - 24, H: math.Ceil(h)}
		y = l.speechErr.Bottom()
	}
	l.height = math.Ceil(y + 10)
	return l
}

func (p *popup) height() float64 { return p.layout().height }

// boxHeight is how tall a text box must be to show text set within width
// without scrolling, up to most.
func boxHeight(text string, width, most float64) float64 {
	style := ui.Text{Family: boxFont, Size: boxSize, Wrap: true}
	_, line := ui.MeasureText("国", style, 0)
	// A trailing line break starts a line the layout would not count.
	if text == "" || strings.HasSuffix(text, "\n") {
		text += "国"
	}
	_, h := ui.MeasureText(text, style, width-4)
	return math.Min(math.Ceil(math.Max(h, line))+4, most)
}

// place puts a popup of the given height in DIPs below and to the right
// of the cursor, inside the work area of its display.
func place(cx, cy int, height float64, mon ui.Monitor) ui.Bounds {
	s, area := mon.Scale, mon.Work
	gap := int(popupGap * s)
	w := int(popupWidth*s + 0.5)
	h := min(int(height*s+0.5), area.H-2*gap)
	x := max(min(cx+gap, area.X+area.W-gap-w), area.X+gap)
	y := max(min(cy+gap, area.Y+area.H-gap-h), area.Y+gap)
	return ui.Bounds{X: x, Y: y, W: w, H: h}
}

// fit resizes the shown popup in place to its content, moving it up if it
// would run off the bottom of the screen.
func (p *popup) fit() {
	w := p.win
	if w == nil {
		return
	}
	mon := w.Monitor()
	s, area := w.Scale(), mon.Work
	gap := int(popupGap * s)
	b := w.Bounds()
	b.W = int(popupWidth*s + 0.5)
	b.H = min(int(p.height()*s+0.5), area.H-2*gap)
	b.Y = max(min(b.Y, area.Y+area.H-gap-b.H), area.Y+gap)
	if b != w.Bounds() {
		w.SetBounds(b)
	}
}

// Frame draws the popup and handles its input.
func (p *popup) Frame(f *ui.Frame) {
	w := f.Window()
	if w != p.win {
		return
	}
	t := f.Theme
	s := p.m.currentSettings()

	if f.KeyPressed(ui.KeyEscape, 0) {
		p.hide()
		return
	}
	if ev := f.Event(); ev.Kind == ui.EvKeyDown && ev.ID == "source" && ev.Key == ui.KeyReturn && ev.Mods == 0 {
		// An input method confirming a candidate sends another key code,
		// so this Enter is the user's own.
		f.TakeKey()
		p.translate(p.source.Text)
	}

	l := p.layout()

	// The header moves the window.
	fw, _ := f.Size()
	header := ui.Rect{W: fw, H: 40}
	f.DragWindow(header)
	f.Text(ui.Rect{X: 12, W: 100, H: 40}, "翻译", ui.Text{Size: 12, Color: t.MutedForeground, Middle: true})
	closeR := ui.Rect{X: fw - 6 - iconButtonSize, Y: 6, W: iconButtonSize, H: iconButtonSize}
	pinR := closeR
	pinR.X -= iconButtonSize + 4
	pinGlyph := rune(ui.IconPin)
	if p.pinned {
		pinGlyph = ui.IconUnpin
	}
	if iconButton(f, "pin", pinR, pinGlyph, p.pinned) {
		p.pinned = !p.pinned
	}
	if iconButton(f, "close", closeR, ui.IconClose, false) {
		p.hide()
		return
	}

	if p.notice != "" {
		f.FillRound(l.notice, design.Radius-2, t.Accent)
		f.Text(l.notice.InsetXY(12, 8), p.notice, ui.Text{Size: 13, Wrap: true, Color: t.AccentForeground})
	}

	// The source text and its language.
	card(f, l.sourceCard)
	if f.TextBox("source", l.sourceBox, &p.source, t.Card) {
		p.source.Changed = false
	}
	bar := l.sourceBar
	by := bar.Y + (bar.H-pickerHeight)/2
	detected := ""
	if s.Source == autoDetect && p.result == resultDone {
		detected = p.translation.Source
	}
	opts := languageOptions(s.Source, true, detected)
	label := opts[max(optionIndex(opts, s.Source), 0)].label
	pw, v, ok := picker(f, "source.lang", bar.X, by, label, opts, s.Source)
	if ok {
		p.changeSettings(func(s *Settings) { s.Source = v }, p.source.Text)
	}
	if p.source.Text == "" {
		// A multi-line edit control shows no placeholder, so the hint sits
		// in the bar below it.
		f.Text(ui.Rect{X: bar.X + pw + 8, Y: bar.Y, W: bar.W - pw - 8, H: bar.H}, "输入要翻译的文字，按回车翻译",
			ui.Text{Size: 12, Color: t.MutedForeground.WithAlpha(0.7), Middle: true})
	}
	if p.result == resultDone {
		r := ui.Rect{X: bar.Right() - iconButtonSize, Y: bar.Y + (bar.H-iconButtonSize)/2, W: iconButtonSize, H: iconButtonSize}
		if p.speakButton(f, "source.speak", r, "source") {
			p.toggleSpeech("source", p.source.Text, p.translation.Source)
		}
	}

	// The translation, its language and the engine.
	card(f, l.resultCard)
	bar = l.resultBar
	by = bar.Y + (bar.H-pickerHeight)/2
	target := s.Target
	if p.result == resultDone {
		target = p.translation.Target
	}
	opts = languageOptions(target, false, "")
	label = opts[max(optionIndex(opts, target), 0)].label
	_, v, ok = picker(f, "target.lang", bar.X, by, label, opts, target)
	if ok {
		p.changeSettings(func(s *Settings) { s.Target = v }, p.source.Text)
	}
	engines := engineOptions(s)
	label = engineName(s.Engine)
	ew := pickerWidth(label)
	if _, v, ok := picker(f, "engine", bar.Right()-ew, by, label, engines, s.Engine); ok {
		p.changeSettings(func(s *Settings) { s.Engine = v }, p.source.Text)
	}
	if p.result == resultLoading {
		spinner(f, ui.Rect{X: bar.Right() - ew - 22, Y: bar.Y + (bar.H-14)/2, W: 14, H: 14}, t.MutedForeground)
		if !p.spinning {
			p.spinning = true
			w.After(90, func() {
				p.spinning = false
				w.Invalidate()
			})
		}
	}
	switch p.result {
	case resultError:
		f.Text(l.resultBody, p.errText, ui.Text{Size: 13, Wrap: true, Color: t.Destructive})
	case resultDone:
		f.TextBox("result", l.resultBody, &p.output, t.Card)
		a := l.resultActions
		r := ui.Rect{X: a.Right() - iconButtonSize, Y: a.Y + (a.H-iconButtonSize)/2, W: iconButtonSize, H: iconButtonSize}
		glyph := rune(ui.IconCopy)
		if p.copied {
			glyph = ui.IconCheck
		}
		if iconButton(f, "copy", r, glyph, p.copied) {
			p.copyResult()
		}
		r.X -= iconButtonSize + 2
		if p.speakButton(f, "result.speak", r, "result") {
			p.toggleSpeech("result", p.translation.Text, p.translation.Target)
		}
	default:
		f.Text(l.resultBody, "译文", ui.Text{Family: boxFont, Size: boxSize, Color: t.MutedForeground.WithAlpha(0.6), Middle: true})
	}

	// The swap button sits on the seam between the cards, over both.
	swapTo := p.swapLanguage(s)
	sr := l.swap
	res := ui.Response{}
	if swapTo != "" {
		res = f.Interact("swap", sr)
		f.SetCursor(sr, ui.CursorHand)
	}
	f.FillEllipse(sr, t.Background)
	f.StrokeEllipse(sr, 1, t.Border)
	fg := t.MutedForeground
	switch {
	case swapTo == "":
		fg = fg.WithAlpha(0.4)
	case res.Hovered:
		fg = t.Foreground
	}
	f.Icon(sr, ui.IconSwap, 13, fg)
	if res.Clicked {
		p.swap(s)
	}

	if p.speechErr != "" {
		f.Text(l.speechErr, p.speechErr, ui.Text{Size: 13, Wrap: true, Color: t.Destructive})
	}

	// Follow the content's height; the resize waits until this frame is
	// drawn.
	want := int(p.layout().height*w.Scale() + 0.5)
	if b := w.Bounds(); abs(b.H-want) > 1 && !p.fitting {
		p.fitting = true
		ui.Post(func() {
			p.fitting = false
			if p.win == w {
				p.fit()
			}
		})
	}
}

// speakButton is the read aloud button of the text named key.
func (p *popup) speakButton(f *ui.Frame, id string, r ui.Rect, key string) bool {
	if p.speaking == key {
		return iconButton(f, id, r, ui.IconStop, true)
	}
	return iconButton(f, id, r, ui.IconSpeaker, false)
}

// card draws the bordered panel the texts sit on.
func card(f *ui.Frame, r ui.Rect) {
	f.FillRound(r, design.Radius-2, f.Theme.Card)
	f.StrokeRound(r, design.Radius-2, 1, f.Theme.Border)
}

// crlf gives text the line breaks the edit control shows.
func crlf(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
