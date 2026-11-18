package core

import (
	"errors"
	"math"
	"slices"
	"strings"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
)

// recorder is the recording of a new combination for a hotkey. While it
// runs, the hotkeys are released and the keyboard is captured, so that a
// combination that is bound, or that the system or another program uses,
// is recorded instead of acted on.
type recorder struct {
	// id is the hotkey being recorded, "" when none is.
	id string
	// held are the modifiers held while recording, in accelerator order.
	held []string
	// saving is set once a combination was recorded: the hotkeys stay
	// released until all of its keys are let go, as registered while still
	// held, the next key repeat would trigger the new hotkey.
	saving bool
	// stop ends the key capture; nil when no recording or saving runs.
	stop func()
	// hit is set by the recorder being recorded when this frame's click
	// landed on it; a click elsewhere cancels.
	hit bool
}

func (d *dashboard) startRecording(id string) {
	if d.rec.stop != nil {
		return
	}
	d.core.hotkeys.Suspend()
	// The keys arrive off the UI thread; Post never blocks, which keeps
	// the capture's hook from stalling the keyboard.
	stop, err := platform.CaptureKeys(func(k platform.KeyPress) { ui.Post(func() { d.onKey(k) }) })
	if err != nil {
		d.core.log.Error("capture keys", "error", err)
		d.core.hotkeys.Resume()
		d.fail(errors.New("无法录制快捷键：" + err.Error()))
		return
	}
	d.rec = recorder{id: id, stop: stop}
}

// endRecording ends a recording or the wait after one, gives the keyboard
// back and registers the hotkeys again.
func (d *dashboard) endRecording() {
	if d.rec.stop == nil {
		return
	}
	d.rec.stop()
	d.rec = recorder{}
	d.core.hotkeys.Resume()
	d.invalidate()
}

func (d *dashboard) onKey(k platform.KeyPress) {
	// A key may arrive after the capture ended.
	if d.rec.stop == nil {
		return
	}
	switch {
	case k.Down:
		d.press(k.Modifiers, k.Key)
	case d.rec.id != "":
		d.rec.held = k.Modifiers
	case k.Idle:
		d.endRecording()
	}
	d.invalidate()
}

func (d *dashboard) press(mods []string, key string) {
	if d.rec.saving {
		return
	}
	switch {
	case key == "escape" && len(mods) == 0:
		d.endRecording()
	case key == "":
		d.rec.held = mods
	default:
		id := d.rec.id
		d.rec.id, d.rec.held, d.rec.saving = "", nil, true
		d.setHotkey(id, strings.Join(append(slices.Clone(mods), key), "+"), false)
	}
}

// setHotkey binds a hotkey; an empty accelerator unbinds it. When another
// hotkey or a system shortcut has the combination, it asks before taking
// it.
func (d *dashboard) setHotkey(id, accelerator string, force bool) {
	err := d.core.hotkeys.Set(id, accelerator, force)
	d.refresh()
	var c *hotkey.Conflict
	if !force && errors.As(err, &c) {
		retry := func() { d.setHotkey(id, accelerator, true) }
		if c.Hotkey != "" {
			d.confirm = &confirmation{title: err.Error(), text: "「" + c.Hotkey + "」的快捷键会被清除。", label: "改用这里", run: retry}
		} else {
			d.confirm = &confirmation{
				title: err.Error(),
				text:  "「" + c.System + "」在其他程序里都会失效；有些组合由系统优先处理，可能仍然无法使用。",
				label: "仍然使用", run: retry,
			}
		}
		return
	}
	d.fail(err)
}

func (d *dashboard) resetHotkey(id string) {
	err := d.core.hotkeys.Reset(id)
	d.refresh()
	d.fail(err)
}

// hotkeysPage lists every hotkey by owner and records new combinations.
type hotkeysPage struct{ d *dashboard }

func (p hotkeysPage) Draw(f *ui.Frame, x, y, w float64) float64 {
	d := p.d
	intro := ui.Text{Size: 13, Wrap: true, Color: f.Theme.MutedForeground}
	const text = "全局快捷键在任何程序里都能用。点击一个快捷键，按下新的组合即可修改，按 Esc 取消。"
	_, ih := ui.MeasureText(text, intro, w-8)
	f.Text(ui.Rect{X: x + 4, Y: y, W: w - 8, H: ih}, text, intro)
	y += ih

	var owners []string
	groups := map[string][]hotkey.Status{}
	for _, h := range d.hotkeys {
		if _, ok := groups[h.OwnerName]; !ok {
			owners = append(owners, h.OwnerName)
		}
		groups[h.OwnerName] = append(groups[h.OwnerName], h)
	}
	for _, owner := range owners {
		rows := make([]ui.Row, 0, len(groups[owner]))
		for _, h := range groups[owner] {
			rows = append(rows, d.hotkeyRow(f, h))
		}
		y = f.Section(x, y+sectionGap, w, owner, rows)
	}
	return y
}

func (d *dashboard) hotkeyRow(f *ui.Frame, h hotkey.Status) ui.Row {
	caps, hint := d.recorderContent(h)
	recW := math.Max(144, capsWidth(caps, hint)+16)
	row := ui.Row{
		Title: h.Label, Note: h.Error,
		ControlW: recW + 6 + 32 + 6 + 32,
		Control: func(r ui.Rect) {
			d.drawRecorder(f, h, ui.Rect{X: r.X, Y: r.Y, W: recW, H: r.H}, caps, hint)
			// The buttons keep their place when hidden, so rows line up.
			bx := r.X + recW + 6
			if h.Accelerator != h.Default {
				if f.Button("hotkey.reset."+h.ID, ui.Rect{X: bx, Y: r.Y, W: 32, H: 32}, ui.Button{Icon: ui.IconUndo, Kind: ui.ButtonGhost}) {
					d.resetHotkey(h.ID)
				}
			}
			if h.Accelerator != "" {
				if f.Button("hotkey.clear."+h.ID, ui.Rect{X: bx + 38, Y: r.Y, W: 32, H: 32}, ui.Button{Icon: ui.IconClose, Kind: ui.ButtonGhost}) {
					d.setHotkey(h.ID, "", false)
				}
			}
		},
	}
	if !h.Active {
		row.Badge, row.BadgeTone = "模块未启用", f.Theme.MutedForeground
	}
	return row
}

// recorderContent returns the keycaps the recorder of h shows, or the text
// it shows instead.
func (d *dashboard) recorderContent(h hotkey.Status) (caps []string, hint string) {
	switch {
	case d.rec.id == h.ID && len(d.rec.held) > 0:
		for _, m := range d.rec.held {
			caps = append(caps, modifierLabel(m))
		}
		return caps, ""
	case d.rec.id == h.ID:
		return nil, "按下快捷键…"
	case h.Accelerator != "":
		return keycaps(h.Accelerator), ""
	}
	return nil, "未设置"
}

func (d *dashboard) drawRecorder(f *ui.Frame, h hotkey.Status, r ui.Rect, caps []string, hint string) {
	t := f.Theme
	recording := d.rec.id == h.ID
	res := f.Interact("hotkey."+h.ID, r)
	f.SetCursor(r, ui.CursorHand)
	if recording && res.Pressed {
		d.rec.hit = true
	}
	if recording {
		f.StrokeRound(r.Inset(-3), 9, 3, t.Ring)
		f.FillRound(r, 6, t.Card)
		f.FillRound(r, 6, t.Primary.WithAlpha(0.05))
		f.StrokeRound(r, 6, 1, t.Primary)
	} else {
		bg := t.Secondary.WithAlpha(t.Secondary.A * 0.6)
		if res.Hovered {
			bg = t.Secondary
		}
		f.FillRound(r, 6, bg)
		f.StrokeRound(r, 6, 1, t.Border)
	}
	if hint != "" {
		col := t.MutedForeground
		if recording {
			col = t.Primary
		}
		f.Text(r, hint, ui.Text{Size: 13, Color: col, Align: ui.AlignCenter, Middle: true})
	} else {
		drawKeycaps(f, r, caps)
	}
	if res.Clicked && !recording {
		d.startRecording(h.ID)
	}
}

var capText = ui.Text{Size: 12, Weight: ui.Medium}

func capWidth(c string) float64 {
	w, _ := ui.MeasureText(c, capText, 0)
	return math.Max(24, math.Ceil(w)+12)
}

// capsWidth is how wide the keycaps, or the text shown instead, are.
func capsWidth(caps []string, hint string) float64 {
	if hint != "" {
		w, _ := ui.MeasureText(hint, ui.Text{Size: 13}, 0)
		return w
	}
	w := 0.0
	for i, c := range caps {
		if i > 0 {
			w += 4
		}
		w += capWidth(c)
	}
	return w
}

// drawKeycaps draws keys centred in r, each with a deeper bottom edge.
func drawKeycaps(f *ui.Frame, r ui.Rect, caps []string) {
	t := f.Theme
	x := r.X + (r.W-capsWidth(caps, ""))/2
	y := r.Y + (r.H-24)/2
	for _, c := range caps {
		cw := capWidth(c)
		k := ui.Rect{X: x, Y: y, W: cw, H: 24}
		f.FillRound(k, 6, t.Border)
		f.FillRound(ui.Rect{X: k.X + 1, Y: k.Y + 1, W: k.W - 2, H: k.H - 3}, 5, t.Background)
		st := capText
		st.Color, st.Align, st.Middle = t.Foreground, ui.AlignCenter, true
		f.Text(ui.Rect{X: k.X, Y: k.Y, W: k.W, H: k.H - 1}, c, st)
		x += cw + 4
	}
}

var keyLabels = map[string]string{
	"space": "Space", "return": "Enter", "tab": "Tab", "backspace": "Backspace", "delete": "Delete",
	"escape": "Esc", "home": "Home", "end": "End", "page up": "PgUp", "page down": "PgDn",
	"left": "←", "right": "→", "up": "↑", "down": "↓",
}

// keycaps splits an accelerator, in the hotkey package's syntax, into the
// labels shown on its keys.
func keycaps(accelerator string) []string {
	parts := strings.Split(accelerator, "+")
	key := parts[len(parts)-1]
	caps := make([]string, 0, len(parts))
	for _, m := range parts[:len(parts)-1] {
		caps = append(caps, modifierLabel(m))
	}
	if l, ok := keyLabels[key]; ok {
		return append(caps, l)
	}
	return append(caps, strings.ToUpper(key))
}

func modifierLabel(m string) string {
	if m == "Super" {
		return "Win"
	}
	return m
}
