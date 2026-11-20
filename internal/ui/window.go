package ui

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/shirumesu/arubox/internal/design"
)

// Bounds is a rectangle in physical screen pixels.
type Bounds struct{ X, Y, W, H int }

// Contains reports whether the physical point is inside b.
func (b Bounds) Contains(x, y int) bool { return x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H }

// Monitor is one display.
type Monitor struct {
	Bounds Bounds // the whole display
	Work   Bounds // without the taskbar
	Scale  float64
}

// MonitorAt returns the display nearest a physical point.
func MonitorAt(x, y int) Monitor {
	h, _, _ := procMonitorFromPoint.Call(uintptr(uint32(int32(x)))|uintptr(uint32(int32(y)))<<32, monitorDefaultToNearest)
	return monitorInfoOf(h)
}

func monitorInfoOf(h uintptr) Monitor {
	mi := monitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(h, uintptr(unsafe.Pointer(&mi)))
	var dx, dy uint32
	shcore.NewProc("GetDpiForMonitor").Call(h, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy)))
	if dx == 0 {
		dx = 96
	}
	return Monitor{Bounds: boundsOf(mi.Monitor), Work: boundsOf(mi.Work), Scale: float64(dx) / 96}
}

func boundsOf(r rect) Bounds {
	return Bounds{int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)}
}

// CursorPos returns the mouse position in physical screen pixels.
func CursorPos() (x, y int) {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return int(p.X), int(p.Y)
}

// Kind is the frame a window has.
type Kind int

// Window kinds.
const (
	// Popup has no title bar; the system draws its shadow and, on
	// Windows 11, rounds its corners.
	Popup Kind = iota
	// Normal has the system's title bar and border.
	Normal
	// Overlay has no frame, shadow or rounding: it covers exactly its
	// bounds, as a screenshot overlay does.
	Overlay
)

// WindowOptions configures NewWindow.
type WindowOptions struct {
	Kind  Kind
	Title string
	// Bounds in physical pixels.
	Bounds Bounds
	// Center ignores Bounds' position and centres the window in the work
	// area of the display under the mouse.
	Center bool
	// SizeInDIPs makes Bounds.W and H device-independent, scaled by the
	// display the window opens on.
	SizeInDIPs bool
	MinW, MinH int // in DIPs
	Topmost    bool
	// NoActivate keeps the window from taking the focus when it is shown
	// or clicked.
	NoActivate bool
	// Taskbar shows the window on the taskbar.
	Taskbar   bool
	Resizable bool
	// SmallCorners asks Windows 11 for the small corner radius.
	SmallCorners bool
	// Icon is PNG or ICO data for the title bar and the taskbar.
	Icon []byte
}

// View draws a window and reacts to its input. Frame runs for every paint
// and every input event, on the UI thread.
type View interface {
	Frame(f *Frame)
}

// ViewFunc adapts a function to View.
type ViewFunc func(f *Frame)

// Frame implements View.
func (v ViewFunc) Frame(f *Frame) { v(f) }

// Window is a top-level window. Its methods must be called on the UI
// thread unless noted.
type Window struct {
	hwnd   uintptr
	opts   WindowOptions
	view   View
	target comObject // ID2D1DCRenderTarget, drawing into dib
	canvas *Canvas
	// dib is the window's picture, which target draws into and frames copy
	// to the screen; dc holds it. Both are freed whenever the window is
	// hidden.
	dc, dib    uintptr
	dibW, dibH int
	gen        int
	scale      float64
	width      int // client size in physical pixels
	height     int
	// icons are the big and small icons made from opts.Icon, destroyed
	// with the window.
	icons [2]uintptr
	// drawErr is the drawing failure last logged, until a frame succeeds.
	drawErr string

	// OnClose runs when the user asks to close the window; returning false
	// keeps it open. Without it, closing hides the window.
	OnClose func() bool
	// OnDeactivate runs when the window loses the focus.
	OnDeactivate func()
	// OnDestroy runs once the window is gone.
	OnDestroy func()
	// OnMove runs after the user moved or resized the window.
	OnMove func()

	input        inputState
	regions      []region // this frame's interactive regions, back to front
	last         []region // the previous frame's
	drags        []dragArea
	lastDrags    []dragArea
	seq          int
	cursors      []cursorRegion
	lastCursors  []cursorRegion
	edits        map[string]*edit
	scrolls      map[string]float64
	again        bool
	painting     bool
	releaseLater bool
	tracking     bool
	destroyed    bool
	keyTaken     bool
	focusEdit    string
	timers       map[uintptr]func()
	nextTimer    uintptr
}

var (
	shcore          = windows.NewLazySystemDLL("shcore.dll")
	procDestroyIcon = user32.NewProc("DestroyIcon")
)

var allWindows = map[*Window]bool{}

// NewWindow creates a hidden window. Call on the UI thread.
func NewWindow(opts WindowOptions, view View) (*Window, error) {
	if err := factories(); err != nil {
		return nil, err
	}
	w := &Window{opts: opts, view: view, edits: map[string]*edit{}, scrolls: map[string]float64{}, timers: map[uintptr]func(){}}
	w.input.down = -1
	class := "arubox.window"
	if err := registerClass(class, csDblClks, idcArrow); err != nil {
		return nil, err
	}
	var style, ex uint32
	switch opts.Kind {
	case Normal:
		style = wsOverlappedWindow
		if !opts.Resizable {
			style &^= wsThickFrame | wsMaximizeBox
		}
	case Popup:
		// A caption and thick frame keep the system shadow and rounding;
		// the frame itself is removed in WM_NCCALCSIZE.
		style = wsPopup | wsCaption | wsThickFrame | wsSysMenu
	case Overlay:
		style = wsPopup
	}
	style |= wsClipChildren
	if opts.Topmost {
		ex |= wsExTopmost
	}
	if opts.NoActivate {
		ex |= wsExNoActivate
	}
	if opts.Taskbar {
		ex |= wsExAppWindow
	} else {
		ex |= wsExToolWindow
	}
	b := opts.Bounds
	if opts.SizeInDIPs || opts.Center {
		var m Monitor
		if opts.Center {
			m = MonitorAt(CursorPos())
		} else {
			m = MonitorAt(b.X+b.W/2, b.Y+b.H/2)
		}
		if opts.SizeInDIPs {
			b.W, b.H = int(float64(b.W)*m.Scale+0.5), int(float64(b.H)*m.Scale+0.5)
		}
		if opts.Center {
			b.X = m.Work.X + (m.Work.W-b.W)/2
			b.Y = m.Work.Y + (m.Work.H-b.H)/2
		}
	}
	hwnd, err := createWindow(w, ex, style, class, opts.Title, b.X, b.Y, b.W, b.H, 0)
	if err != nil {
		return nil, err
	}
	w.hwnd = hwnd
	w.scale = w.dpiScale()
	if opts.Kind == Popup {
		m := margins{1, 1, 1, 1}
		procDwmExtendFrame.Call(hwnd, uintptr(unsafe.Pointer(&m)))
		pref := uint32(dwmwcpRound)
		if opts.SmallCorners {
			pref = dwmwcpRoundSmall
		}
		procDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&pref)), 4)
		procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
	}
	if opts.Kind == Overlay {
		pref := uint32(dwmwcpDoNotRound)
		procDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&pref)), 4)
	}
	if len(opts.Icon) > 0 {
		if big := iconFromData(opts.Icon, 32); big != 0 {
			procSendMessageW.Call(hwnd, 0x0080, 1, big) // WM_SETICON ICON_BIG
			w.icons[0] = big
		}
		if small := iconFromData(opts.Icon, 16); small != 0 {
			procSendMessageW.Call(hwnd, 0x0080, 0, small)
			w.icons[1] = small
		}
	}
	w.applyTheme()
	w.updateSize()
	allWindows[w] = true
	return w, nil
}

// HWND returns the window handle.
func (w *Window) HWND() uintptr { return w.hwnd }

// Scale returns physical pixels per DIP.
func (w *Window) Scale() float64 { return w.scale }

// Show shows the window; activate gives it the focus.
func (w *Window) Show(activate bool) {
	if w.destroyed {
		return
	}
	if activate && !w.opts.NoActivate {
		procShowWindow.Call(w.hwnd, swShow)
		procSetForegroundWindow.Call(w.hwnd)
		w.restoreFocus()
	} else {
		procShowWindow.Call(w.hwnd, swShowNoActivate)
	}
	// Paint now rather than when the queue gets to it, so the window never
	// shows unpainted.
	w.frame(Event{})
}

// Hide hides the window.
func (w *Window) Hide() {
	if !w.destroyed {
		procShowWindow.Call(w.hwnd, swHide)
		// A hidden window keeps no picture: it is drawn again when shown.
		w.releaseTarget()
	}
}

// Visible reports whether the window is shown.
func (w *Window) Visible() bool {
	if w.destroyed {
		return false
	}
	r, _, _ := procIsWindowVisible.Call(w.hwnd)
	return r != 0
}

// Active reports whether the window has the focus.
func (w *Window) Active() bool {
	fg, _, _ := procGetForegroundWindow.Call()
	return fg == w.hwnd
}

// Close destroys the window.
func (w *Window) Close() {
	if !w.destroyed {
		procDestroyWindow.Call(w.hwnd)
	}
}

// Closed reports whether the window was destroyed.
func (w *Window) Closed() bool { return w.destroyed }

// SetTitle changes the title.
func (w *Window) SetTitle(s string) { procSetWindowTextW.Call(w.hwnd, utf16Ptr(s)) }

// SetBounds moves and sizes the window, in physical pixels.
func (w *Window) SetBounds(b Bounds) {
	procSetWindowPos.Call(w.hwnd, 0, uintptr(b.X), uintptr(b.Y), uintptr(b.W), uintptr(b.H), swpNoZOrder|swpNoActivate)
}

// SetTopmost keeps the window above others, or not.
func (w *Window) SetTopmost(on bool) {
	after := ^uintptr(1) // HWND_NOTOPMOST
	if on {
		after = ^uintptr(0) // HWND_TOPMOST
	}
	procSetWindowPos.Call(w.hwnd, after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

// Bounds returns the window's rectangle in physical pixels.
func (w *Window) Bounds() Bounds {
	var r rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	return boundsOf(r)
}

// Monitor returns the display the window is on.
func (w *Window) Monitor() Monitor {
	h, _, _ := procMonitorFromWindow.Call(w.hwnd, monitorDefaultToNearest)
	return monitorInfoOf(h)
}

// Invalidate asks for a repaint. It is safe from any goroutine.
func (w *Window) Invalidate() {
	if w.painting {
		w.again = true
		return
	}
	procInvalidateRect.Call(w.hwnd, 0, 0)
}

// After runs f on the UI thread once ms milliseconds have passed, unless
// the window is gone by then.
func (w *Window) After(ms int, f func()) {
	w.nextTimer++
	id := w.nextTimer
	w.timers[id] = f
	procSetTimer.Call(w.hwnd, id, uintptr(ms), 0)
}

func (w *Window) dpiScale() float64 {
	d, _, _ := procGetDpiForWindow.Call(w.hwnd)
	if d == 0 {
		return 1
	}
	return float64(d) / 96
}

func (w *Window) applyTheme() {
	dark := uint32(0)
	if DarkMode() {
		dark = 1
	}
	procDwmSetWindowAttribute.Call(w.hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&dark)), 4)
	for _, e := range w.edits {
		e.theme()
	}
}

func (w *Window) updateSize() {
	var r rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	w.width, w.height = int(r.Right), int(r.Bottom)
}

// ensureTarget makes the picture and the target drawing into it, at the
// window's current size.
//
// A DC target over a DIB of the window's own is used rather than an HWND
// target: Direct2D's software rasterizer pools the buffers of an HWND
// target, and of bitmaps copied into any target, for the life of the
// process, which kept about 100 MB after one full-screen screenshot.
// Memory the window owns is given back when it is freed.
func (w *Window) ensureTarget() error {
	width, height := max(w.width, 1), max(w.height, 1)
	if w.target != nil && w.dibW == width && w.dibH == height {
		return nil
	}
	if w.target == nil {
		props := d2dRenderTargetProperties{Type: d2dTargetSoftware, PixelFormat: d2dPixelFormat{dxgiFormatB8G8R8A8, d2dAlphaPremultiplied}}
		var t comObject
		if !succeeded(call(d2dFactory, slotFactoryCreateDCRenderTarget, uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&t)))) {
			return errors.New("创建绘图目标失败")
		}
		w.gen++
		c, err := newCanvasOn(t, w.gen)
		if err != nil {
			release(t)
			return err
		}
		w.target, w.canvas = t, c
	}
	w.releaseDIB()
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return errors.New("创建绘图表面失败")
	}
	bmi := bitmapInfoHeader{Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	bmi.Size = uint32(unsafe.Sizeof(bmi))
	var bits uintptr
	dib, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 {
		procDeleteDC.Call(dc)
		return errors.New("创建绘图表面失败")
	}
	procSelectObject.Call(dc, dib)
	r := rect{0, 0, int32(width), int32(height)}
	if !succeeded(call(w.target, slotDCRTBindDC, dc, uintptr(unsafe.Pointer(&r)))) {
		procDeleteObject.Call(dib)
		procDeleteDC.Call(dc)
		return errors.New("创建绘图目标失败")
	}
	w.dc, w.dib, w.dibW, w.dibH = dc, dib, width, height
	return nil
}

func (w *Window) releaseDIB() {
	if w.dc != 0 {
		procDeleteDC.Call(w.dc)
		procDeleteObject.Call(w.dib)
		w.dc, w.dib, w.dibW, w.dibH = 0, 0, 0, 0
	}
}

func (w *Window) releaseTarget() {
	if w.painting {
		// Hidden or closed by its own view: the frame still draws on the
		// target, so it is released once the frame is done.
		w.releaseLater = true
		return
	}
	if w.canvas != nil {
		w.canvas.release()
		w.canvas = nil
	}
	release(w.target)
	w.target = nil
	w.releaseDIB()
}

// present copies the picture to the screen.
func (w *Window) present() {
	hdc, _, _ := procGetDC.Call(w.hwnd)
	if hdc == 0 {
		return
	}
	procBitBlt.Call(hdc, 0, 0, uintptr(w.dibW), uintptr(w.dibH), w.dc, 0, 0, srcCopy)
	procReleaseDC.Call(w.hwnd, hdc)
}

// frame runs the view once, drawing the window, with ev as its input.
func (w *Window) frame(ev Event) {
	// A hidden window keeps no picture, and input that still reaches it,
	// such as the release of the click that hid it or the mouse leaving
	// it, would make one again for as long as it stays hidden.
	if w.destroyed || w.painting || !w.Visible() {
		return
	}
	if err := w.ensureTarget(); err != nil {
		w.drawn(err)
		return
	}
	w.painting = true
	callf(w.target, slotRTSetDpi, "ff", f32(96*w.scale), f32(96*w.scale))
	call(w.target, slotRTBeginDraw)
	c := w.canvas
	c.Scale = w.scale
	c.Theme = design.Theme(DarkMode())
	c.ox, c.oy = 0, 0
	c.setTransform(1)
	c.Clear(c.Theme.Background)
	f := &Frame{Canvas: c, w: w, ev: ev}
	f.size = Point{float64(w.width) / w.scale, float64(w.height) / w.scale}
	w.seq = 0
	w.regions = w.regions[:0]
	w.drags = w.drags[:0]
	w.cursors = w.cursors[:0]
	for _, e := range w.edits {
		e.used = false
	}
	w.view.Frame(f)
	for c.clips > 0 {
		c.PopClip()
	}
	hr := call(w.target, slotRTEndDraw, 0, 0)
	w.painting = false
	w.last, w.regions = w.regions, w.last
	w.lastDrags, w.drags = w.drags, w.lastDrags
	w.lastCursors, w.cursors = w.cursors, w.lastCursors
	for id, e := range w.edits {
		if !e.used {
			e.hide()
			if w.focusEdit == id {
				w.focusEdit = ""
			}
		}
	}
	switch {
	case w.releaseLater:
		w.releaseLater = false
		w.releaseTarget()
	case uint32(hr) == d2dErrRecreateTarget:
		w.releaseTarget()
		w.again = true
	case !succeeded(hr):
		w.drawn(fmt.Errorf("Direct2D 绘制失败（0x%08X）", uint32(hr)))
	default:
		w.drawn(nil)
		w.present()
	}
	procValidateRect.Call(w.hwnd, 0)
	if w.again {
		w.again = false
		procInvalidateRect.Call(w.hwnd, 0, 0)
	}
}

// drawn records how drawing the window went: a failure is logged once, not
// on every frame that fails the same way, until a frame succeeds.
func (w *Window) drawn(err error) {
	if err == nil {
		w.drawErr = ""
		return
	}
	if msg := err.Error(); msg != w.drawErr {
		w.drawErr = msg
		logger.Error("draw window", "window", w.opts.Title, "error", err)
	}
}

func (w *Window) handle(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmNCCalcSize:
		if wp != 0 && w.opts.Kind == Popup {
			return 0, true
		}
	case wmNCActivate:
		if w.opts.Kind == Popup {
			// Keep the frame from repainting over the client area.
			r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), wp, ^uintptr(0))
			return r, true
		}
	case wmNCHitTest:
		return w.hitTest(lp), true
	case 0x0112: // WM_SYSCOMMAND
		if wp&0xFFF0 == 0xF100 && w.opts.Kind != Normal { // SC_KEYMENU
			// Releasing Alt would otherwise put a window without a menu in
			// menu mode, and the next key typed only beeps: the launcher's
			// Alt+Shift+Space left exactly that behind.
			return 0, true
		}
	case wmEraseBkgnd:
		return 1, true
	case wmPaint:
		var ps paintStruct
		procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		w.frame(Event{})
		return 0, true
	case wmSize:
		w.updateSize()
		w.Invalidate()
		return 0, true
	case wmMove:
		return 0, false
	case 0x0232: // WM_EXITSIZEMOVE
		if w.OnMove != nil {
			w.OnMove()
		}
		return 0, false
	case wmDpiChanged:
		w.scale = float64(loword(wp)) / 96
		r := (*rect)(unsafe.Pointer(lp))
		procSetWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate)
		// The next frame gives each text box a font for the new scale,
		// deleting its old one.
		for _, e := range w.edits {
			e.place(e.r, e.vis, w)
		}
		w.Invalidate()
		return 0, true
	case wmGetMinMaxInfo:
		if w.opts.MinW > 0 || w.opts.MinH > 0 {
			mm := (*minMaxInfo)(unsafe.Pointer(lp))
			s := w.scale
			if s == 0 {
				s = 1
			}
			mm.MinTrackSize = point{int32(float64(w.opts.MinW) * s), int32(float64(w.opts.MinH) * s)}
		}
		return 0, true
	case wmMouseActivate:
		if w.opts.NoActivate {
			return maNoActivate, true
		}
	case wmActivate:
		if loword(wp) == 0 {
			if w.OnDeactivate != nil {
				w.OnDeactivate()
			}
		} else {
			w.restoreFocus()
		}
		w.Invalidate()
		return 0, false
	case wmSetCursor:
		if loword(lp) == htClient && wp == hwnd {
			p := w.mouse()
			for i := len(w.lastCursors) - 1; i >= 0; i-- {
				if w.lastCursors[i].r.Contains(p) {
					setCursor(w.lastCursors[i].c)
					return 1, true
				}
			}
			setCursor(CursorArrow)
			return 1, true
		}
	case wmMouseMove:
		if !w.tracking {
			t := trackMouseEventInfo{Flags: tmeLeave, Hwnd: hwnd}
			t.Size = uint32(unsafe.Sizeof(t))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
			w.tracking = true
		}
		w.input.inside = true
		w.mouseEvent(EvMouseMove, lp, 0)
		return 0, true
	case wmMouseLeave:
		w.tracking = false
		w.input.inside = false
		w.frame(Event{Kind: EvMouseLeave})
		return 0, true
	case wmLButtonDown, wmRButtonDown, wmMButtonDown:
		procSetCapture.Call(hwnd)
		b := map[uint32]int{wmLButtonDown: 0, wmRButtonDown: 1, wmMButtonDown: 2}[m]
		w.mouseEvent(EvMouseDown, lp, b)
		return 0, true
	case wmLButtonDblClk:
		procSetCapture.Call(hwnd)
		w.mouseEvent(EvDoubleClick, lp, 0)
		return 0, true
	case wmLButtonUp, wmRButtonUp, wmMButtonUp:
		b := map[uint32]int{wmLButtonUp: 0, wmRButtonUp: 1, wmMButtonUp: 2}[m]
		w.mouseEvent(EvMouseUp, lp, b)
		// Release only after the click is handled: ReleaseCapture sends
		// WM_CAPTURECHANGED at once, which drops the press as cancelled.
		procReleaseCapture.Call()
		return 0, true
	case wmCaptureChanged:
		if w.input.down >= 0 {
			w.input.down = -1
			w.input.active = ""
			w.Invalidate()
		}
		return 0, true
	case wmMouseWheel:
		var p point
		p.X, p.Y = int32(loword(lp)), int32(hiword(lp))
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&p)))
		w.input.pos = Point{float64(p.X) / w.scale, float64(p.Y) / w.scale}
		w.frame(Event{Kind: EvWheel, Wheel: float64(hiword(wp)) / 120, Pos: w.input.pos, Mods: mods()})
		return 0, true
	case wmKeyDown, wmSysKeyDown:
		if w.key(int(wp)) {
			return 0, true
		}
		if m == wmSysKeyDown {
			return 0, false
		}
		return 0, true
	case wmChar:
		w.frame(Event{Kind: EvChar, Char: rune(wp)})
		return 0, true
	case wmTimer:
		procKillTimer.Call(hwnd, wp)
		if f := w.timers[wp]; f != nil {
			delete(w.timers, wp)
			f()
		}
		return 0, true
	case wmCommand:
		if code := uint32(wp >> 16); code == enSetFocus || code == enKillFocus {
			for id, e := range w.edits {
				if e.hwnd == lp {
					if code == enSetFocus {
						w.focusEdit = id
					}
					w.Invalidate()
					break
				}
			}
			return 0, true
		}
		if uint32(wp>>16) == enChange {
			for id, e := range w.edits {
				if e.hwnd == lp {
					e.changed()
					w.frame(Event{Kind: EvTextChanged, ID: id})
					break
				}
			}
			return 0, true
		}
	case wmCtlColorEdit, wmCtlColorStatic:
		for _, e := range w.edits {
			if e.hwnd == lp {
				return e.colors(wp), true
			}
		}
	case wmClose:
		if w.OnClose != nil {
			if w.OnClose() {
				w.Close()
			}
		} else {
			w.Hide()
		}
		return 0, true
	case wmDestroy:
		w.destroyed = true
		w.releaseTarget()
		for _, e := range w.edits {
			e.release()
		}
		// A window does not destroy the icons it was given.
		for i, h := range w.icons {
			if h != 0 {
				procDestroyIcon.Call(h)
				w.icons[i] = 0
			}
		}
		delete(allWindows, w)
		if w.OnDestroy != nil {
			w.OnDestroy()
		}
		return 0, false
	}
	return 0, false
}

func (w *Window) mouse() Point {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	procScreenToClient.Call(w.hwnd, uintptr(unsafe.Pointer(&p)))
	return Point{float64(p.X) / w.scale, float64(p.Y) / w.scale}
}

func (w *Window) mouseEvent(kind EventKind, lp uintptr, button int) {
	pos := Point{float64(loword(lp)) / w.scale, float64(hiword(lp)) / w.scale}
	w.input.pos = pos
	ev := Event{Kind: kind, Pos: pos, Button: button, Mods: mods()}
	switch kind {
	case EvMouseDown, EvDoubleClick:
		w.input.down = button
		w.input.downPos = pos
		w.input.active = w.hotAt(pos)
		if w.input.active == "" || !w.editIs(w.input.active) {
			// A click outside every text box takes the focus off them, so
			// window keys work.
			if w.focusEdit != "" && !w.opts.NoActivate {
				procSetFocus.Call(w.hwnd)
				w.focusEdit = ""
			}
		}
	}
	w.frame(ev)
	if kind == EvMouseUp {
		w.input.down = -1
		w.input.active = ""
		w.frame(Event{})
	}
}

func (w *Window) editIs(id string) bool { _, ok := w.edits[id]; return ok }

// hotAt finds the topmost region of the last frame under p.
func (w *Window) hotAt(p Point) string {
	for i := len(w.last) - 1; i >= 0; i-- {
		if w.last[i].r.Contains(p) {
			return w.last[i].id
		}
	}
	return ""
}

func (w *Window) hitTest(lp uintptr) uintptr {
	var p point
	p.X, p.Y = int32(loword(lp)), int32(hiword(lp))
	procScreenToClient.Call(w.hwnd, uintptr(unsafe.Pointer(&p)))
	x, y := int(p.X), int(p.Y)
	if w.opts.Resizable && w.opts.Kind == Popup {
		edge := int(6 * w.scale)
		left, right := x < edge, x >= w.width-edge
		top, bottom := y < edge, y >= w.height-edge
		switch {
		case top && left:
			return htTopLeft
		case top && right:
			return htTopRight
		case bottom && left:
			return htBottomLeft
		case bottom && right:
			return htBottomRight
		case left:
			return htLeft
		case right:
			return htRight
		case top:
			return htTop
		case bottom:
			return htBottom
		}
	}
	if w.opts.Kind == Normal {
		r, _, _ := procDefWindowProcW.Call(w.hwnd, wmNCHitTest, 0, lp)
		if r != htClient {
			return r
		}
	}
	dp := Point{float64(x) / w.scale, float64(y) / w.scale}
	// A drag area moves the window unless something interactive drawn
	// later sits over it.
	for i := len(w.lastDrags) - 1; i >= 0; i-- {
		d := w.lastDrags[i]
		if !d.r.Contains(dp) {
			continue
		}
		for j := len(w.last) - 1; j >= 0; j-- {
			if w.last[j].r.Contains(dp) && w.last[j].seq > d.seq {
				return htClient
			}
		}
		return htCaption
	}
	return htClient
}

func (w *Window) key(vk int) bool {
	w.keyTaken = false
	w.frame(Event{Kind: EvKeyDown, Key: vk, Mods: mods()})
	return w.keyTaken
}

func (w *Window) restoreFocus() {
	if w.focusEdit != "" {
		if e := w.edits[w.focusEdit]; e != nil && e.hwnd != 0 {
			procSetFocus.Call(e.hwnd)
		}
	}
}

func mods() Mods {
	var m Mods
	if keyDown(vkControl) {
		m |= ModifierCtrl
	}
	if keyDown(vkShift) {
		m |= ModifierShift
	}
	if keyDown(vkMenu) {
		m |= ModifierAlt
	}
	if keyDown(vkLWin) || keyDown(vkRWin) {
		m |= ModifierWin
	}
	return m
}

// dialogKey gives a window first say over keys typed into its text boxes,
// so arrows, Enter and Escape can drive a list while the box has the
// focus. It reports whether the window took the key.
func dialogKey(m *msg) bool {
	if m.Message != wmKeyDown && m.Message != wmSysKeyDown && m.Message != wmChar {
		return false
	}
	parent, _, _ := procGetParent.Call(m.Hwnd)
	if parent == 0 {
		return false
	}
	h, ok := handlers[parent].(*Window)
	if !ok {
		return false
	}
	for id, e := range h.edits {
		if e.hwnd == m.Hwnd {
			if m.Message == wmChar {
				// Ctrl+A, Enter and Escape would only beep in a single-line
				// box.
				c := m.WParam
				return c == 1 || (!e.multi && (c == '\r' || c == 0x1B))
			}
			k := mods()
			if typingKey(int(m.WParam), k) {
				// A character, or a key for the input method: no view takes
				// these from a box, and the change it makes runs a frame of
				// its own, so a frame here would only draw twice.
				return false
			}
			h.keyTaken = false
			h.frame(Event{Kind: EvKeyDown, Key: int(m.WParam), Mods: k, ID: id})
			return h.keyTaken
		}
	}
	return false
}

// typingKey reports whether a key typed into a text box only types: a
// character key without Ctrl, Alt or Win, or one the input method takes.
func typingKey(vk int, m Mods) bool {
	switch {
	case vk == 0xE5 || vk == 0xE7: // VK_PROCESSKEY, VK_PACKET
		return true
	case m&^ModifierShift != 0:
		return false
	}
	return vk == KeySpace || vk >= '0' && vk <= '9' || vk >= 'A' && vk <= 'Z' ||
		vk >= 0x60 && vk <= 0x6F || vk >= 0xBA && vk <= 0xC0 || vk >= 0xDB && vk <= 0xDF || vk == 0xE2
}

// themeChanged repaints every window in the new theme.
func init() {
	OnThemeChange(func(bool) {
		for w := range allWindows {
			w.applyTheme()
			w.Invalidate()
		}
	})
}

// Cursor is a mouse pointer shape.
type Cursor int

// Cursors.
const (
	CursorArrow Cursor = idcArrow
	CursorHand  Cursor = idcHand
	CursorText  Cursor = idcIBeam
	CursorCross Cursor = idcCross
	CursorMove  Cursor = idcSizeAll
	CursorNWSE  Cursor = idcSizeNWSE
	CursorNESW  Cursor = idcSizeNESW
	CursorWE    Cursor = idcSizeWE
	CursorNS    Cursor = idcSizeNS
)

var cursorHandles = map[Cursor]uintptr{}

func setCursor(c Cursor) {
	h, ok := cursorHandles[c]
	if !ok {
		h, _, _ = procLoadCursorW.Call(0, uintptr(c))
		cursorHandles[c] = h
	}
	procSetCursor.Call(h)
}

type cursorRegion struct {
	r Rect
	c Cursor
}

var _ = math.Abs
