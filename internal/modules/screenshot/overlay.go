package screenshot

import (
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"strings"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/ui"
)

// start is what a capture hands the editor.
type start struct {
	display ui.Monitor
	image   platform.Image
	// windows are the windows on screen, front to back, for picking one
	// with a click.
	windows []platform.Rect
	// translate offers translating the selection's text, when the
	// translation module is on.
	translate bool
	// cursor is the mouse position in physical pixels relative to the
	// display.
	cursor ui.Point
}

var (
	colors = []ui.Color{hexColor(0xf43f5e), hexColor(0xf59e0b), hexColor(0x22c55e), hexColor(0x3b82f6), hexColor(0xffffff), hexColor(0x111111)}
	widths = []float64{2, 4, 8}
	// textSizes are the text sizes for the three widths.
	textSizes = map[float64]float64{2: 16, 4: 22, 8: 32}
	accent    = hexColor(0x3b82f6)
	white     = ui.RGBA(255, 255, 255, 1)
	black     = ui.RGBA(0, 0, 0, 1)
)

const (
	// clickSlop is how far a press may move and still be a click, which
	// picks a window.
	clickSlop    = 3.0
	minSelection = 4.0
	// iconRedo is Segoe Fluent's Redo glyph, which the ui package does not
	// name.
	iconRedo = ''
)

type dragKind int

const (
	dragNone dragKind = iota
	dragSelect
	dragMove
	dragResize
	dragShape
)

// overlay is the editor: a window over the whole display showing it
// frozen, where the user selects and annotates. It is made for one
// capture and closed when the capture ends, which lets go of the screen
// sized picture it holds.
type overlay struct {
	m         *Module
	win       *ui.Window
	image     platform.Image // the capture, in physical pixels
	shot      *ui.Image      // the same pixels, drawn
	display   ui.Bounds
	scale     float64
	size      ui.Point // the display in DIPs
	windows   []ui.Rect
	translate bool
	ended     bool
	// away is set while the editor is hidden for the picture to be copied
	// or saved, and may come back.
	away bool

	hasSel    bool
	sel       ui.Rect
	selecting bool // a new selection is being dragged out
	selBox    ui.Rect
	hasHover  bool // a window is under the mouse
	hover     ui.Rect
	tool      tool
	shapes    []*shape
	undone    []*shape
	draft     *shape

	editing   bool // a text annotation is being typed
	editAt    ui.Point
	edit      ui.Field
	editColor ui.Color

	// pending is the action under way, whose button spins meanwhile.
	pending string
	spin    int
	// hasText shows the text read from the selection, for copying.
	hasText bool
	ocr     ui.Field
	message string

	drag       dragKind
	dragFrom   ui.Point
	dragStart  ui.Rect
	dragHandle handle
	moved      bool

	tip       string
	tipAnchor ui.Rect
}

// openOverlay shows the editor on a capture. Call on the UI thread.
func openOverlay(m *Module, s start) (*overlay, error) {
	o := &overlay{m: m, image: s.image, display: s.display.Bounds, translate: s.translate}
	o.shot = ui.NewImage(s.image.Width, s.image.Height, s.image.Pixels)
	b := o.display
	win, err := ui.NewWindow(ui.WindowOptions{Kind: ui.Overlay, Title: "截图", Bounds: b, Topmost: true}, o)
	if err != nil {
		return nil, err
	}
	o.win = win
	o.scale = win.Scale()
	o.size = ui.Point{X: float64(b.W) / o.scale, Y: float64(b.H) / o.scale}
	o.windows = windowsOn(b, o.scale, s.windows)
	o.updateHover(ui.Point{X: s.cursor.X / o.scale, Y: s.cursor.Y / o.scale})
	// Switching to another window ends the capture, as Esc does; hiding to
	// copy or save does not.
	win.OnDeactivate = func() {
		if !o.away {
			o.end()
		}
	}
	win.OnClose = func() bool {
		o.end()
		return false
	}
	win.OnDestroy = o.release
	win.Show(true)
	return o, nil
}

// end closes the editor. It may run inside a frame, so the window is
// hidden at once but destroyed once the frame is done.
func (o *overlay) end() {
	if o.ended {
		return
	}
	o.ended = true
	o.editing = false
	o.win.Hide()
	ui.Post(o.win.Close)
	if o.m.overlay == o {
		o.m.overlay = nil
	}
	o.m.idle()
}

// release lets go of the pictures once the window is gone.
func (o *overlay) release() {
	o.shot.Release()
	o.shot = nil
	o.dropShapes(o.shapes)
	o.dropShapes(o.undone)
	if o.draft != nil {
		o.dropShapes([]*shape{o.draft})
	}
	o.shapes, o.undone, o.draft = nil, nil, nil
	o.image = platform.Image{}
	// The capture is tens of megabytes on a large display; give it back to
	// the system now rather than whenever the runtime gets to it.
	go debug.FreeOSMemory()
}

func (o *overlay) dropShapes(all []*shape) {
	for _, s := range all {
		if s.mosaic != nil {
			s.mosaic.Release()
			s.mosaic = nil
		}
	}
}

// Frame draws the editor and handles its input.
func (o *overlay) Frame(f *ui.Frame) {
	if o.ended || o.shot == nil {
		return
	}
	screen := f.Bounds()
	o.size = ui.Point{X: screen.W, Y: screen.H}
	ev := f.Event()
	o.keys(f)
	if o.ended {
		return
	}
	// A press anywhere ends typing a text annotation, keeping it.
	pressed := ev.Kind == ui.EvMouseDown || ev.Kind == ui.EvDoubleClick
	consumed := false
	if pressed && o.editing {
		o.commitText(f)
		consumed = true
	}
	canvas := f.Interact("canvas", screen)
	if !consumed {
		o.pointer(f, ev, canvas)
	}
	if o.ended {
		return
	}
	o.tip = ""
	o.draw(f, screen)
	o.cursors(f, screen)
	if o.editing {
		o.textBox(f)
	}
	if o.hasSel && !o.selecting {
		o.toolbar(f, screen)
	}
}

// area is the box outlined: the one being dragged out, the selection or
// the window under the mouse.
func (o *overlay) area() (ui.Rect, bool) {
	switch {
	case o.selecting:
		return o.selBox, true
	case o.hasSel:
		return o.sel, true
	case o.hasHover:
		return o.hover, true
	}
	return ui.Rect{}, false
}

// snap moves a box's edges onto physical pixels, so the dimmed parts
// around it meet without seams.
func (o *overlay) snap(r ui.Rect) ui.Rect {
	s := o.scale
	x0, y0 := math.Round(r.X*s)/s, math.Round(r.Y*s)/s
	x1, y1 := math.Round(r.Right()*s)/s, math.Round(r.Bottom()*s)/s
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func (o *overlay) updateHover(p ui.Point) {
	o.hasHover = false
	for _, w := range o.windows {
		if contains(w, p) {
			o.hover, o.hasHover = w, true
			return
		}
	}
}

func (o *overlay) keys(f *ui.Frame) {
	vk, mods, ok := f.Key()
	if !ok {
		return
	}
	switch f.Event().ID {
	case "text":
		// Enter keeps the text; Shift+Enter starts a new line. An Enter
		// that picks an input method's candidate arrives as another key.
		if vk == ui.KeyReturn && mods == 0 {
			f.TakeKey()
			o.commitText(f)
		} else if vk == ui.KeyEscape {
			f.TakeKey()
			o.editing = false
			f.Blur()
		}
		return
	case "ocr":
		// Typing in the read text keeps its keys, except Esc.
		if vk == ui.KeyEscape {
			f.TakeKey()
			o.end()
		}
		return
	}
	if f.KeyPressed(ui.KeyEscape, 0) {
		o.end()
		return
	}
	if o.editing || o.pending != "" || !o.hasSel {
		return
	}
	switch {
	case f.KeyPressed('Z', ui.ModifierCtrl):
		o.undo()
	case f.KeyPressed('Z', ui.ModifierCtrl|ui.ModifierShift):
		o.redo()
	case f.KeyPressed('C', ui.ModifierCtrl), f.KeyPressed(ui.KeyReturn, 0):
		o.copy()
	case f.KeyPressed('S', ui.ModifierCtrl):
		o.save()
	}
}

func (o *overlay) pointer(f *ui.Frame, ev ui.Event, canvas ui.Response) {
	if o.pending != "" {
		return
	}
	p := clampPoint(ev.Pos, o.size.X, o.size.Y)
	switch {
	case canvas.DoubleClicked && o.hasSel && o.tool == toolNone && contains(o.sel, p):
		o.copy()
		return
	case canvas.Pressed:
		o.press(p)
	case canvas.RightClicked:
		o.stepBack(f)
	}
	switch {
	case ev.Kind == ui.EvMouseMove:
		o.move(p)
	case ev.Kind == ui.EvMouseUp && ev.Button == 0:
		o.finishDrag()
	}
}

func (o *overlay) press(p ui.Point) {
	if o.hasSel && o.tool != toolNone {
		if !contains(o.sel, p) {
			return
		}
		if o.tool == toolText {
			o.startText(p)
			return
		}
		s := &shape{kind: o.tool, from: p, to: p, color: colors[o.m.color], width: o.m.width}
		switch o.tool {
		case toolPen:
			s.points = []ui.Point{p}
		case toolMosaic:
			s.width = 0
		}
		o.draft = s
		o.drag = dragShape
		return
	}
	// Moving or changing the selection makes the text read from it stale.
	o.hasText = false
	if o.hasSel {
		if h, ok := handleAt(o.sel, p); ok {
			o.drag, o.dragHandle, o.dragFrom, o.dragStart = dragResize, h, p, o.sel
			return
		}
		if contains(o.sel, p) {
			o.drag, o.dragFrom, o.dragStart = dragMove, p, o.sel
			return
		}
		// Starting over would lose the annotations.
		if len(o.shapes) > 0 {
			return
		}
	}
	o.drag, o.dragFrom, o.moved = dragSelect, p, false
	if !o.hasSel {
		o.updateHover(p)
	}
}

func (o *overlay) move(p ui.Point) {
	switch o.drag {
	case dragNone:
		if !o.hasSel {
			o.updateHover(p)
		}
	case dragSelect:
		if !o.moved && math.Hypot(p.X-o.dragFrom.X, p.Y-o.dragFrom.Y) < clickSlop {
			return
		}
		o.moved = true
		o.hasHover = false
		o.selecting = true
		o.selBox = boxFrom(o.dragFrom, p)
	case dragMove:
		b := o.dragStart
		b.X += p.X - o.dragFrom.X
		b.Y += p.Y - o.dragFrom.Y
		o.sel = clampBox(b, o.size.X, o.size.Y)
	case dragResize:
		o.sel = clampBox(resize(o.dragStart, o.dragHandle, p.X-o.dragFrom.X, p.Y-o.dragFrom.Y), o.size.X, o.size.Y)
	case dragShape:
		s := o.draft
		if s == nil {
			return
		}
		if s.kind == toolPen {
			s.points = append(s.points, p)
		} else {
			s.to = p
			if s.kind == toolMosaic {
				updateMosaic(s, o.image, o.scale, false)
			}
		}
	}
}

// finishDrag ends a drag when the button is let go.
func (o *overlay) finishDrag() {
	d := o.drag
	o.drag = dragNone
	switch d {
	case dragSelect:
		picked := ui.Rect{W: o.size.X, H: o.size.Y}
		if o.moved {
			picked = o.selBox
		} else if o.hasHover {
			picked = o.hover
		}
		o.selecting, o.hasHover = false, false
		if picked.W >= minSelection && picked.H >= minSelection {
			o.sel, o.hasSel = clampBox(picked, o.size.X, o.size.Y), true
			o.dropShapes(o.shapes)
			o.dropShapes(o.undone)
			o.shapes, o.undone = nil, nil
		}
	case dragShape:
		s := o.draft
		o.draft = nil
		if s == nil {
			return
		}
		if s.kind == toolMosaic {
			updateMosaic(s, o.image, o.scale, true)
		}
		b := boxFrom(s.from, s.to)
		kept := b.W+b.H > 3
		if s.kind == toolPen {
			kept = len(s.points) > 1
		}
		if kept {
			o.add(s)
		} else {
			o.dropShapes([]*shape{s})
		}
	}
}

// add keeps a finished annotation, which forgets what was undone.
func (o *overlay) add(s *shape) {
	o.shapes = append(o.shapes, s)
	o.dropShapes(o.undone)
	o.undone = nil
}

// stepBack is the right click: out of typing, then the tool, then the
// selection, then the capture.
func (o *overlay) stepBack(f *ui.Frame) {
	switch {
	case o.editing:
		o.editing = false
		f.Blur()
	case o.tool != toolNone:
		o.tool = toolNone
	case o.hasSel:
		o.reset()
	default:
		o.end()
	}
}

// reset starts the capture over, with nothing selected.
func (o *overlay) reset() {
	o.hasSel, o.selecting, o.hasHover = false, false, false
	o.tool = toolNone
	o.dropShapes(o.shapes)
	o.dropShapes(o.undone)
	o.shapes, o.undone, o.draft = nil, nil, nil
	o.editing = false
	o.hasText = false
	o.message = ""
	o.drag = dragNone
}

func (o *overlay) undo() {
	if n := len(o.shapes); n > 0 {
		o.undone = append(o.undone, o.shapes[n-1])
		o.shapes = o.shapes[:n-1]
	}
}

func (o *overlay) redo() {
	if n := len(o.undone); n > 0 {
		o.shapes = append(o.shapes, o.undone[n-1])
		o.undone = o.undone[:n-1]
	}
}

func (o *overlay) textSize() float64 {
	if s, ok := textSizes[o.m.width]; ok {
		return s
	}
	return 22
}

func (o *overlay) startText(p ui.Point) {
	size := o.textSize()
	o.editing = true
	// The text pointer's hot spot is the middle of its bar, so the first
	// line is centred on the click, as the pointer stood: placing its top
	// there put the text half a line below the pointer.
	_, lineH := ui.MeasureText("M", ui.Text{Family: textFamily, Size: size}, 0)
	o.editAt = ui.Point{X: p.X, Y: p.Y - lineH/2}
	o.edit = ui.Field{Multiline: true, Ghost: true, Focus: true, Size: size}
	o.editColor = colors[o.m.color]
}

func (o *overlay) commitText(f *ui.Frame) {
	if strings.TrimSpace(o.edit.Text) != "" {
		o.add(&shape{kind: toolText, from: o.editAt, text: o.edit.Text, color: o.editColor, width: o.edit.Size})
	}
	o.editing = false
	f.Blur()
}

// textBox draws the text being typed as it will be kept, with its caret
// or selection, over a box that only takes the typing: a native box cannot
// be see-through, and one with a background of its own hid how the text
// would read on the picture.
func (o *overlay) textBox(f *ui.Frame) {
	size := o.edit.Size
	style := ui.Text{Family: textFamily, Size: size, Color: o.editColor}
	_, lineH := ui.MeasureText("M", style, 0)
	w, h := 0.0, lineH
	if o.edit.Text != "" {
		w, h = ui.MeasureText(o.edit.Text, style, 0)
	}
	h = math.Max(h, float64(strings.Count(o.edit.Text, "\n")+1)*lineH)
	r := ui.Rect{X: o.editAt.X, Y: o.editAt.Y, W: math.Max(w+size, size*2), H: h + 2}
	f.Stroke(r.Inset(-1), 1, white.WithAlpha(0.7), true)
	// Laid out as drawShape lays the kept text out.
	at := ui.Rect{X: o.editAt.X, Y: o.editAt.Y, W: 1e4, H: 1e4}
	start, end, ok := f.TextSelection("text")
	if ok && start != end {
		for _, sel := range ui.TextRanges(at, o.edit.Text, style, start, end) {
			f.Fill(sel, f.Theme.Primary.WithAlpha(0.35))
		}
	}
	f.Text(at, o.edit.Text, style)
	caret := ui.Rect{X: o.editAt.X, Y: o.editAt.Y, H: lineH}
	if ok {
		if cs := ui.TextRanges(at, o.edit.Text, style, end, end); len(cs) > 0 {
			caret = cs[0]
		}
	}
	if start == end {
		f.Fill(ui.Rect{X: caret.X, Y: caret.Y, W: math.Max(1, size/16), H: caret.H}, o.editColor)
	}
	f.TextBox("text", caret, &o.edit, ui.Color{})
}

func (o *overlay) draw(f *ui.Frame, screen ui.Rect) {
	f.Image(o.shot, screen, ui.Rect{}, 1, false)
	dim := black.WithAlpha(0.4)
	area, has := o.area()
	a := o.snap(area)
	if has {
		f.Fill(ui.Rect{W: screen.W, H: a.Y}, dim)
		f.Fill(ui.Rect{Y: a.Bottom(), W: screen.W, H: screen.H - a.Bottom()}, dim)
		f.Fill(ui.Rect{Y: a.Y, W: a.X, H: a.H}, dim)
		f.Fill(ui.Rect{X: a.Right(), Y: a.Y, W: screen.W - a.Right(), H: a.H}, dim)
	} else {
		f.Fill(screen, dim)
	}
	if o.hasSel {
		f.PushClip(o.sel)
		for _, s := range o.shapes {
			drawShape(f.Canvas, s)
		}
		if o.draft != nil {
			drawShape(f.Canvas, o.draft)
		}
		f.PopClip()
	}
	line := math.Max(1, math.Round(o.scale)) / o.scale
	if has {
		f.Stroke(a, line, accent, false)
	}
	if o.hasSel && o.tool == toolNone {
		for h := range handleCount {
			p := handlePoint(o.sel, h)
			r := ui.Rect{X: p.X - 3.5, Y: p.Y - 3.5, W: 7, H: 7}
			f.Fill(r, white)
			f.Stroke(r, line, accent, false)
		}
	}
	shown, showing := o.selBox, o.selecting
	if !showing && o.hasSel {
		shown, showing = o.sel, true
	}
	if !showing && !o.hasHover {
		hint := "拖动框选区域，或点击选择窗口；Esc 取消"
		t := ui.Text{Size: 13, Color: white, Align: ui.AlignCenter, Middle: true}
		tw, th := ui.MeasureText(hint, t, 0)
		r := ui.Rect{X: (screen.W - tw - 32) / 2, Y: 64, W: tw + 32, H: th + 12}
		f.FillRound(r, r.H/2, black.WithAlpha(0.7))
		f.Text(r, hint, t)
	}
	if showing {
		label := fmt.Sprintf("%d × %d", int(math.Round(shown.W*o.scale)), int(math.Round(shown.H*o.scale)))
		t := ui.Text{Size: 11, Color: white, Align: ui.AlignCenter, Middle: true}
		tw, th := ui.MeasureText(label, t, 0)
		r := ui.Rect{X: shown.X, Y: math.Max(shown.Y-22, 2), W: tw + 12, H: th + 4}
		f.FillRound(r, 4, black.WithAlpha(0.7))
		f.Text(r, label, t)
	}
}

// cursors sets the mouse pointer for the part of the screen it is over.
func (o *overlay) cursors(f *ui.Frame, screen ui.Rect) {
	switch {
	case !o.hasSel:
		f.SetCursor(screen, ui.CursorCross)
	case o.tool != toolNone:
		f.SetCursor(screen, ui.CursorArrow)
		c := ui.CursorCross
		if o.tool == toolText {
			c = ui.CursorText
		}
		f.SetCursor(o.sel, c)
	default:
		base := ui.CursorCross
		if len(o.shapes) > 0 {
			base = ui.CursorArrow
		}
		f.SetCursor(screen, base)
		f.SetCursor(o.sel, ui.CursorMove)
		// The first handle in order wins where two overlap, as handleAt
		// picks it, so it is registered last.
		for h := handleCount - 1; h >= 0; h-- {
			p := handlePoint(o.sel, h)
			f.SetCursor(ui.Rect{X: p.X - handleReach, Y: p.Y - handleReach, W: 2 * handleReach, H: 2 * handleReach}, handleCursor(h))
		}
	}
}

// toolButton is a button of the toolbar.
type toolButton struct {
	id       string
	icon     rune // 0 draws an arrow
	label    string
	active   bool
	disabled bool
	primary  bool
	spin     bool
	action   func()
}

// toolbarRow is one of the rows under (or over) the selection.
type toolbarRow struct {
	w, h float64
	draw func(r ui.Rect)
}

const (
	toolSize = 32.0
	toolGap  = 2.0
	// dividerW is a divider with its margins.
	dividerW = 5.0
	rowGap   = 6.0
)

func (o *overlay) toggle(t tool) func() {
	return func() {
		if o.tool == t {
			o.tool = toolNone
		} else {
			o.tool = t
		}
	}
}

func (o *overlay) toolbar(f *ui.Frame, screen ui.Rect) {
	t := o.tool
	buttons := []*toolButton{
		{id: "rect", icon: ui.IconRect, label: "矩形", active: t == toolRect, action: o.toggle(toolRect)},
		{id: "ellipse", icon: ui.IconEllipse, label: "椭圆", active: t == toolEllipse, action: o.toggle(toolEllipse)},
		{id: "arrow", label: "箭头", active: t == toolArrow, action: o.toggle(toolArrow)},
		{id: "pen", icon: ui.IconPen, label: "画笔", active: t == toolPen, action: o.toggle(toolPen)},
		{id: "text", icon: ui.IconText, label: "文字", active: t == toolText, action: o.toggle(toolText)},
		{id: "mosaic", icon: ui.IconMosaic, label: "马赛克", active: t == toolMosaic, action: o.toggle(toolMosaic)},
		nil,
		{id: "undo", icon: ui.IconUndo, label: "撤销（Ctrl+Z）", disabled: len(o.shapes) == 0, action: o.undo},
		{id: "redo", icon: iconRedo, label: "重做（Ctrl+Shift+Z）", disabled: len(o.undone) == 0, action: o.redo},
		nil,
		{id: "read", icon: ui.IconRead, label: "识别文字", active: o.hasText, spin: o.pending == "read", action: o.readText},
	}
	if o.translate {
		buttons = append(buttons, &toolButton{id: "translate", icon: ui.IconTranslate, label: "翻译文字", spin: o.pending == "translate", action: o.translateText})
	}
	buttons = append(buttons,
		&toolButton{id: "pin", icon: ui.IconPin, label: "贴在屏幕上", action: o.pin},
		&toolButton{id: "save", icon: ui.IconSave, label: "保存（Ctrl+S）", action: o.save},
		nil,
		&toolButton{id: "cancel", icon: ui.IconClose, label: "取消（Esc）", action: o.end},
		&toolButton{id: "copy", icon: ui.IconCheck, label: "复制（Ctrl+C）", primary: true, action: o.copy},
	)
	mainW := 8.0 - toolGap
	for _, b := range buttons {
		if b == nil {
			mainW += dividerW + toolGap
		} else {
			mainW += toolSize + toolGap
		}
	}
	rows := []toolbarRow{{w: mainW, h: toolSize + 8, draw: func(r ui.Rect) { o.mainRow(f, r, buttons) }}}
	if t != toolNone && t != toolMosaic {
		w := 16 + float64(len(widths))*24 + float64(len(colors))*20 + float64(len(widths)+len(colors))*rowGap + dividerW
		rows = append(rows, toolbarRow{w: w, h: 36, draw: func(r ui.Rect) { o.pickerRow(f, r) }})
	}
	if o.hasText {
		rows = append(rows, toolbarRow{w: 320, h: 226, draw: func(r ui.Rect) { o.textRow(f, r) }})
	}
	if o.message != "" {
		mt := ui.Text{Size: 12}
		tw, th := ui.MeasureText(o.message, mt, 0)
		rows = append(rows, toolbarRow{w: tw + 24, h: th + 12, draw: func(r ui.Rect) {
			f.FillRound(r, design.Radius, f.Theme.Destructive)
			f.Text(r.InsetXY(12, 0), o.message, ui.Text{Size: 12, Color: white, Middle: true})
		}})
	}

	// The rows go below the selection, or above it when there is no room,
	// or inside its bottom when there is room for neither, aligned to its
	// right edge.
	room := 110.0
	if o.hasText {
		room = 320
	}
	sel := o.sel
	below := sel.Bottom()+room < screen.H
	above := sel.Y > room
	right := math.Min(sel.Right(), screen.W-8)
	y := sel.Bottom() + 8
	if !below {
		y = sel.Bottom() - 8
		if above {
			y = sel.Y - 8
		}
	}
	var first ui.Rect
	for i, row := range rows {
		r := ui.Rect{X: math.Max(8, right-row.w), Y: y, W: row.w, H: row.h}
		if below {
			y += row.h + rowGap
		} else {
			r.Y = y - row.h
			y -= row.h + rowGap
		}
		if i == 0 {
			first = r
		}
		if o.message == "" || i < len(rows)-1 {
			o.panel(f, r)
		}
		f.Interact(fmt.Sprintf("row.%d", i), r)
		f.SetCursor(r, ui.CursorArrow)
		row.draw(r)
	}
	if o.tip != "" {
		o.drawTip(f, screen, first, below)
	}
}

// panel draws a row's card, with a soft shadow.
func (o *overlay) panel(f *ui.Frame, r ui.Rect) {
	th := f.Theme
	f.FillRound(ui.Rect{X: r.X - 1, Y: r.Y + 1, W: r.W + 2, H: r.H + 3}, design.Radius+1, black.WithAlpha(0.10))
	f.FillRound(ui.Rect{X: r.X, Y: r.Y + 2, W: r.W, H: r.H + 2}, design.Radius, black.WithAlpha(0.08))
	f.FillRound(r, design.Radius, th.Background)
	f.StrokeRound(r, design.Radius, 1, th.Border)
}

func (o *overlay) mainRow(f *ui.Frame, r ui.Rect, buttons []*toolButton) {
	th := f.Theme
	x := r.X + 4
	for _, b := range buttons {
		if b == nil {
			f.Fill(ui.Rect{X: x + 2, Y: r.Y + (r.H-20)/2, W: 1, H: 20}, th.Border)
			x += dividerW + toolGap
			continue
		}
		o.toolButton(f, ui.Rect{X: x, Y: r.Y + 4, W: toolSize, H: toolSize}, b)
		x += toolSize + toolGap
	}
}

func (o *overlay) toolButton(f *ui.Frame, r ui.Rect, b *toolButton) {
	th := f.Theme
	var res ui.Response
	if !b.disabled {
		res = f.Interact("tool."+b.id, r)
	} else {
		f.Interact("tool."+b.id+".off", r)
	}
	fg := th.MutedForeground
	var bg ui.Color
	if res.Hovered {
		bg, fg = th.Accent, th.AccentForeground
	}
	if b.active {
		bg, fg = th.Accent, th.Primary
	}
	if b.primary {
		fg = th.Primary
	}
	if b.disabled {
		fg = fg.WithAlpha(0.35)
	}
	f.FillRound(r, design.Radius-2, bg)
	switch {
	case b.spin:
		o.spinner(f, r, fg)
	case b.icon == 0:
		arrowIcon(f, r, fg)
	default:
		f.Icon(r, b.icon, 16, fg)
	}
	if res.Hovered || (b.disabled && f.Hovered(r)) {
		o.tip, o.tipAnchor = b.label, r
	}
	if res.Clicked && !b.disabled {
		b.action()
		f.Invalidate()
	}
}

// arrowIcon draws an arrow pointing up and right, which Segoe Fluent
// does not have.
func arrowIcon(f *ui.Frame, r ui.Rect, col ui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	f.Line(ui.Point{X: cx - 4.5, Y: cy + 4.5}, ui.Point{X: cx + 4, Y: cy - 4}, 1.5, col)
	f.Polyline([]ui.Point{{X: cx - 1.5, Y: cy - 4.5}, {X: cx + 4.5, Y: cy - 4.5}, {X: cx + 4.5, Y: cy + 1.5}}, 1.5, col)
}

// spinner draws a turning arc while an action runs.
func (o *overlay) spinner(f *ui.Frame, r ui.Rect, col ui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	start := float64(o.spin) * 0.35
	pts := make([]ui.Point, 0, 25)
	for i := range 25 {
		a := start + float64(i)/24*1.5*math.Pi
		pts = append(pts, ui.Point{X: cx + 6*math.Cos(a), Y: cy + 6*math.Sin(a)})
	}
	f.Polyline(pts, 1.75, col)
}

// pickerRow offers the widths, or text sizes, and the colours.
func (o *overlay) pickerRow(f *ui.Frame, r ui.Rect) {
	th := f.Theme
	tip := "粗细"
	if o.tool == toolText {
		tip = "字号"
	}
	x := r.X + 8
	for i, w := range widths {
		b := ui.Rect{X: x, Y: r.Y + 6, W: 24, H: 24}
		res := f.Interact(fmt.Sprintf("width.%d", i), b)
		if res.Hovered || o.m.width == w {
			f.FillRound(b, design.Radius-2, th.Accent)
		}
		f.FillEllipse(b.Center(w+2, w+2), th.Foreground)
		if res.Hovered {
			o.tip, o.tipAnchor = tip, b
		}
		if res.Clicked {
			o.m.width = w
		}
		x += 24 + rowGap
	}
	f.Fill(ui.Rect{X: x + 2, Y: r.Y + 8, W: 1, H: 20}, th.Border)
	x += dividerW + rowGap
	for i, c := range colors {
		b := ui.Rect{X: x, Y: r.Y + 8, W: 20, H: 20}
		res := f.Interact(fmt.Sprintf("color.%d", i), b)
		if o.m.color == i {
			f.StrokeEllipse(b.Inset(-3), 2, th.Ring.WithAlpha(1))
		}
		f.FillEllipse(b, c)
		f.StrokeEllipse(b, 1, black.WithAlpha(0.2))
		if res.Hovered {
			o.tip, o.tipAnchor = "颜色", b
		}
		if res.Clicked {
			o.m.color = i
		}
		x += 20 + rowGap
	}
}

// textRow shows the text read from the selection, to edit and copy.
func (o *overlay) textRow(f *ui.Frame, r ui.Rect) {
	th := f.Theme
	area := ui.Rect{X: r.X + 6, Y: r.Y + 6, W: r.W - 12, H: 176}
	bg := th.Muted.WithAlpha(0.6)
	f.FillRound(area, design.Radius-2, bg)
	f.TextBox("ocr", area.InsetXY(8, 6), &o.ocr, bg)
	y := area.Bottom() + 6
	f.Text(ui.Rect{X: r.X + 10, Y: y, W: 160, H: 32}, "可以先修改再复制", ui.Text{Size: 11, Color: th.MutedForeground, Middle: true})
	btn := ui.Button{Label: "复制文字", Icon: ui.IconCopy, Kind: ui.ButtonPrimary, Small: true, Disabled: o.pending != ""}
	bw := btn.Width()
	if f.Button("copytext", ui.Rect{X: r.Right() - 6 - bw, Y: y, W: bw, H: 32}, btn) {
		o.copyText()
	}
}

func (o *overlay) drawTip(f *ui.Frame, screen, row ui.Rect, below bool) {
	t := ui.Text{Size: 12, Color: white, Align: ui.AlignCenter, Middle: true}
	tw, th := ui.MeasureText(o.tip, t, 0)
	w, h := tw+16, th+8
	x := math.Min(math.Max(o.tipAnchor.X+o.tipAnchor.W/2-w/2, 4), screen.W-w-4)
	// Away from the rows that follow, so it covers none of them.
	y := row.Bottom() + 4
	if below {
		y = row.Y - 4 - h
	}
	r := ui.Rect{X: x, Y: y, W: w, H: h}
	f.FillRound(r, 6, black.WithAlpha(0.8))
	f.Text(r, o.tip, t)
}

// run marks an action as under way, spinning its button.
func (o *overlay) run(name string) {
	o.pending = name
	o.message = ""
	o.tick()
}

func (o *overlay) tick() {
	if o.ended || o.pending == "" {
		return
	}
	o.spin++
	o.win.Invalidate()
	o.win.After(50, o.tick)
}

// failed ends the action under way with an error, keeping the editor open.
func (o *overlay) failed(err error) {
	o.pending = ""
	o.message = err.Error()
	o.win.Invalidate()
}

// flatten renders the selection with its annotations at the capture's
// full resolution, with the same drawing code as the screen.
func (o *overlay) flatten() (picture, error) {
	px := pixelRect(o.sel, o.scale, o.image.Width, o.image.Height)
	if px.Width == 0 || px.Height == 0 {
		return picture{}, errors.New("没有选区")
	}
	base := o.image.Crop(px.X, px.Y, px.Width, px.Height)
	if len(o.shapes) > 0 {
		over, err := ui.Render(px.Width, px.Height, func(c *ui.Canvas) {
			c.Zoom(o.scale, -float64(px.X), -float64(px.Y), func() {
				for _, s := range o.shapes {
					drawShape(c, s)
				}
			})
		})
		if err != nil {
			return picture{}, err
		}
		composite(base.Pixels, over.Pixels)
	}
	return picture(base), nil
}

// copy puts the finished picture on the clipboard.
func (o *overlay) copy() {
	if o.pending != "" || !o.hasSel {
		return
	}
	p, err := o.flatten()
	if err != nil {
		o.failed(err)
		return
	}
	o.deliver("copy screenshot", func() (bool, error) { return true, p.copy() })
}

// save asks where to save the finished picture and saves it there.
func (o *overlay) save() {
	if o.pending != "" || !o.hasSel {
		return
	}
	p, err := o.flatten()
	if err != nil {
		o.failed(err)
		return
	}
	o.deliver("save screenshot", func() (bool, error) { return o.m.save(p) })
}

// deliver runs send on a goroutine of its own and ends the capture once
// send has done its part. The editor hides meanwhile, out of the way of
// the save dialog, and comes back as it was when send is cancelled
// (false) or fails, showing why: the selection and its annotations are
// not lost with it.
func (o *overlay) deliver(what string, send func() (bool, error)) {
	o.message = ""
	o.away = true
	o.win.Hide()
	go func() {
		done, err := send()
		ui.Post(func() {
			if o.ended {
				// The module was turned off meanwhile.
				if err != nil {
					o.m.log.Warn(what, "error", err)
				}
				return
			}
			o.away = false
			if done && err == nil {
				o.end()
				return
			}
			if err != nil {
				o.failed(err)
			}
			o.win.Show(true)
		})
	}()
}

// pin pins the finished picture where it was selected.
func (o *overlay) pin() {
	if o.pending != "" || !o.hasSel {
		return
	}
	p, err := o.flatten()
	if err != nil {
		o.failed(err)
		return
	}
	px := pixelRect(o.sel, o.scale, o.image.Width, o.image.Height)
	b := ui.Bounds{X: o.display.X + px.X, Y: o.display.Y + px.Y, W: px.Width, H: px.Height}
	o.end()
	m := o.m
	ui.Post(func() { m.pins.add(m, p, b) })
}

// selection returns the selection's part of the capture, unannotated, to
// read its text.
func (o *overlay) selection() platform.Image {
	px := pixelRect(o.sel, o.scale, o.image.Width, o.image.Height)
	return o.image.Crop(px.X, px.Y, px.Width, px.Height)
}

// readText reads the selection's text and shows it for copying. The
// editor stays open meanwhile, so it can show a failure.
func (o *overlay) readText() {
	if o.pending != "" || !o.hasSel {
		return
	}
	img := o.selection()
	o.run("read")
	go func() {
		lines, err := o.m.recognize(img)
		ui.Post(func() {
			if o.ended {
				return
			}
			if err != nil {
				o.failed(err)
				return
			}
			o.pending = ""
			o.hasText = true
			o.ocr = ui.Field{Text: joinLines(lines), Multiline: true, Size: 13}
			o.win.Invalidate()
		})
	}()
}

// copyText copies the read text, as edited, and closes the editor.
func (o *overlay) copyText() {
	if o.pending != "" {
		return
	}
	text := o.ocr.Text
	o.deliver("copy screenshot text", func() (bool, error) { return true, platform.CopyText(text) })
}

// translateText reads the selection's text and shows it in the
// translation popup, with lines broken inside a sentence joined back.
func (o *overlay) translateText() {
	if o.pending != "" || !o.hasSel {
		return
	}
	tr := o.m.translator
	if tr == nil || !tr.Available() {
		o.failed(errors.New("翻译未开启"))
		return
	}
	img := o.selection()
	o.run("translate")
	go func() {
		lines, err := o.m.recognize(img)
		ended := false
		ui.Sync(func() {
			ended = o.ended
			if ended {
				return
			}
			if err != nil {
				o.failed(err)
				return
			}
			o.end()
		})
		if !ended && err == nil {
			tr.Open(platform.Paragraphs(lines))
		}
	}()
}
