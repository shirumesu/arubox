package platform

import (
	"encoding/binary"
	"errors"
	"math"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The desktop layer takes over the desktop's icons: it hides File
// Explorer's icon list and lays one layered window per monitor in its
// place, inside the desktop's window, so it stays under every other window,
// survives Win+D and shows the wallpaper through. Everything about it runs
// on one UI thread of its own; the handler is called there.

var (
	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procCallWindowProcW          = user32.NewProc("CallWindowProcW")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procFindWindowExW            = user32.NewProc("FindWindowExW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procIsWindow                 = user32.NewProc("IsWindow")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procScreenToClient           = user32.NewProc("ScreenToClient")
	procGetTextMetricsW          = gdi32.NewProc("GetTextMetricsW")
	procEnumDisplayMonitors      = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procUpdateLayeredWindowIndir = user32.NewProc("UpdateLayeredWindowIndirect")
	procSetTimer                 = user32.NewProc("SetTimer")
	procKillTimer                = user32.NewProc("KillTimer")
	procTrackMouseEvent          = user32.NewProc("TrackMouseEvent")
	procSetCapture               = user32.NewProc("SetCapture")
	procReleaseCapture           = user32.NewProc("ReleaseCapture")
	procSetFocus                 = user32.NewProc("SetFocus")
	procSetCursor                = user32.NewProc("SetCursor")
	procLoadCursorW              = user32.NewProc("LoadCursorW")
	procGetKeyState              = user32.NewProc("GetKeyState")
	procRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procTrackPopupMenuEx         = user32.NewProc("TrackPopupMenuEx")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procCreateFontW              = gdi32.NewProc("CreateFontW")
	shcore                       = windows.NewLazySystemDLL("shcore.dll")
	procGetDpiForMonitor         = shcore.NewProc("GetDpiForMonitor")
	procOleInitialize            = ole32.NewProc("OleInitialize")
	procOleUninitialize          = ole32.NewProc("OleUninitialize")
	procRegisterDragDrop         = ole32.NewProc("RegisterDragDrop")
	procRevokeDragDrop           = ole32.NewProc("RevokeDragDrop")
)

const (
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmSetFocus       = 0x0007
	wmKillFocus      = 0x0008
	wmSetCursor      = 0x0020
	wmSettingChange  = 0x001A
	wmMeasureItem    = 0x002C
	wmDrawItem       = 0x002B
	wmChar           = 0x0102
	wmTimer          = 0x0113
	wmInitMenuPopup  = 0x0117
	wmMenuSelect     = 0x011F
	wmMenuChar       = 0x0120
	wmMouseMove      = 0x0200
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmLButtonDblClk  = 0x0203
	wmRButtonDown    = 0x0204
	wmRButtonUp      = 0x0205
	wmMButtonDown    = 0x0207
	wmMouseWheel     = 0x020A
	wmCaptureChanged = 0x0215
	wmMouseLeave     = 0x02A3
	wmDisplayChange  = 0x007E
	wmSetFont        = 0x0030
	wmApp            = 0x8000
	wmRun            = wmApp + 1
	wmPaintLayer     = wmApp + 2
	wmShellNotify    = wmApp + 3

	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsPopup         = 0x80000000
	wsBorder        = 0x00800000
	wsClipSiblings  = 0x04000000
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000
	wsExTopmost     = 0x00000008
	esCenter        = 0x0001
	esMultiline     = 0x0004
	esAutoVScroll   = 0x0040
	esAutoHScroll   = 0x0080
	csDblClks       = 0x0008
	swHide          = 0
	swShow          = 5
	swpNoActivate   = 0x0010
	swpNoZOrder     = 0x0004
	swpShowWindow   = 0x0040
	hwndTop         = 0
	ulwAlpha        = 2
	acSrcAlpha      = 1
	tmeLeave        = 0x2
	htClient        = 1
	gwlpWndProc     = ^uintptr(3) // -4
	emSetSel        = 0x00B1
	vkReturn        = 0x0D
	vkEscape        = 0x1B
	vkShift         = 0x10
	vkControl       = 0x11
	mfString        = 0x0000
	mfGrayed        = 0x0001
	mfChecked       = 0x0008
	mfPopup         = 0x0010
	mfSeparator     = 0x0800
	mfByPosition    = 0x0400
	tpmReturnCmd    = 0x0100
	tpmRightButton  = 0x0002
	mdtEffectiveDpi = 0
	timerAnimate    = 1
	timerReattach   = 2
	frameMS         = 15
)

type rect struct{ Left, Top, Right, Bottom int32 }

type point struct{ X, Y int32 }

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

// DesktopMonitor is a monitor the desktop layer covers, in physical pixels
// of the screen.
type DesktopMonitor struct {
	ID     string
	Bounds Box
	Work   Box
	Scale  float64
}

// PointerKind is what a pointer event reports.
type PointerKind int

// The pointer events. PointerLeave reports that the pointer left the
// layer, PointerCancel a press that ended without its release, such as
// when a menu took the mouse.
const (
	PointerMove PointerKind = iota
	PointerDown
	PointerUp
	PointerDoubleClick
	PointerWheel
	PointerLeave
	PointerCancel
)

// PointerEvent is a mouse event, at screen pixels.
type PointerEvent struct {
	Kind PointerKind
	// Button is 1 for the left, 2 for the right and 3 for the middle one.
	Button           int
	X, Y             float64
	Ctrl, Shift, Alt bool
	// Wheel is how far the wheel turned, in notches; up is positive.
	Wheel float64
}

// KeyEvent is a key pressed while the layer has the keyboard.
type KeyEvent struct {
	// Key is the virtual-key code.
	Key              uint32
	Ctrl, Shift, Alt bool
}

// Cursor is a mouse pointer shape.
type Cursor uintptr

// Cursors, by system ID.
const (
	CursorArrow    Cursor = 32512
	CursorMove     Cursor = 32646
	CursorSizeWE   Cursor = 32644
	CursorSizeNS   Cursor = 32645
	CursorSizeNWSE Cursor = 32642
	CursorSizeNESW Cursor = 32643
)

// MenuItem is an entry of a menu the desktop shows.
type MenuItem struct {
	// ID is what Menu returns when the entry is chosen; it must be
	// positive. A separator has none.
	ID        int
	Text      string
	Checked   bool
	Disabled  bool
	Separator bool
	Sub       []MenuItem
}

// DesktopHandler is what the desktop layer calls, on its UI thread.
type DesktopHandler interface {
	// Attached hands over the desktop on its UI thread, before anything
	// else is called.
	Attached(d *Desktop)
	// Monitors reports the monitors when the layer attaches and whenever
	// they change. Everything is to be drawn again after it.
	Monitors(monitors []DesktopMonitor)
	// Paint draws the part of the screen inside dirty.
	Paint(c *Canvas, dirty Box)
	Pointer(e PointerEvent)
	Key(e KeyEvent)
	// Tick is called every frame while Animate is on.
	Tick()
	// DropAt says what a drag over the point would drop onto. internal
	// reports a drag of the layer's own items.
	DropAt(x, y float64, internal bool) DropTarget
	// Dropped reports a drop that was done.
	Dropped(x, y float64, t DropTarget)
	// ShellChanged reports that watched folders changed; renames pairs
	// old and new parsing names of items renamed.
	ShellChanged(renames [][2]string)
	// EditDone ends an Edit: ok is false when it was cancelled.
	EditDone(text string, ok bool)
}

type surface struct {
	hwnd    uintptr
	monitor DesktopMonitor
	canvas  *Canvas
	dirty   Box
	full    bool // the window has been given a whole picture
	drop    *dropTarget
}

// Desktop is the attached desktop layer.
type Desktop struct {
	handler DesktopHandler

	mu    sync.Mutex
	queue []func()

	// The rest belongs to the UI thread.
	host       uintptr
	defView    uintptr
	folderView uintptr
	surfaces   []*surface
	paintSent  bool
	animating  bool
	cursor     Cursor
	alpha      byte
	tracking   uintptr // the surface tracking the mouse leaving
	detaching  bool
	menu       comObject // the shell menu being shown, for its messages
	edit       *editBox
	watch      uint32
	watched    []string
	dragging   bool        // the layer's own drag is under way
	explorer   *iconLayout // where File Explorer had its icons
	done       chan struct{}
}

var (
	desktopMu      sync.Mutex
	activeDesktop  *Desktop
	taskbarCreated uintptr
	hostProc       = windows.NewCallback(hostWndProc)
	surfaceProc    = windows.NewCallback(surfaceWndProc)
	editProc       = windows.NewCallback(editWndProc)
)

const (
	hostClass    = "AruboxDesktopHost"
	surfaceClass = "AruboxDesktopSurface"
	editClass    = "AruboxDesktopEdit"
)

// AttachDesktop takes over the desktop's icons until Detach.
func AttachDesktop(h DesktopHandler) (*Desktop, error) {
	desktopMu.Lock()
	defer desktopMu.Unlock()
	if activeDesktop != nil {
		return nil, errors.New("桌面已被接管")
	}
	d := &Desktop{handler: h, alpha: 255, cursor: CursorArrow, done: make(chan struct{})}
	ready := make(chan error, 1)
	go d.run(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	activeDesktop = d
	return d, nil
}

// Detach gives the desktop back to File Explorer.
func (d *Desktop) Detach() {
	d.Do(func() {
		d.detach()
		if d.explorer != nil {
			_ = d.explorer.restore()
		}
		procPostQuitMessage.Call(0)
	})
	<-d.done
	desktopMu.Lock()
	activeDesktop = nil
	desktopMu.Unlock()
}

// Do runs f on the UI thread.
func (d *Desktop) Do(f func()) {
	d.mu.Lock()
	d.queue = append(d.queue, f)
	d.mu.Unlock()
	procPostMessageW.Call(d.host, wmRun, 0, 0)
}

func (d *Desktop) run(ready chan<- error) {
	runtime.LockOSThread()
	defer close(d.done)
	if hr, _, _ := procOleInitialize.Call(0); !succeeded(hr) {
		ready <- errors.New("初始化 OLE 失败")
		return
	}
	defer procOleUninitialize.Call()
	if err := registerClasses(); err != nil {
		ready <- err
		return
	}
	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	taskbarCreated, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	cls, _ := windows.UTF16PtrFromString(hostClass)
	host, _, _ := procCreateWindowExW.Call(wsExToolWindow, uintptr(unsafe.Pointer(cls)), 0, wsPopup, 0, 0, 0, 0, 0, 0, 0, 0)
	if host == 0 {
		ready <- errors.New("创建桌面窗口失败")
		return
	}
	d.host = host
	desktops.Store(host, d)
	d.handler.Attached(d)
	if err := d.attach(); err != nil {
		procDestroyWindow.Call(host)
		desktops.Delete(host)
		ready <- err
		return
	}
	ready <- nil
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	d.unwatch()
	procDestroyWindow.Call(host)
	desktops.Delete(host)
}

// desktops finds the Desktop of a host window.
var desktops sync.Map

var classesOnce sync.Once
var classesErr error

func registerClasses() error {
	classesOnce.Do(func() {
		arrow, _, _ := procLoadCursorW.Call(0, uintptr(CursorArrow))
		for _, c := range []struct {
			name  string
			proc  uintptr
			style uint32
		}{{hostClass, hostProc, 0}, {surfaceClass, surfaceProc, csDblClks}, {editClass, editProc, 0}} {
			n, _ := windows.UTF16PtrFromString(c.name)
			wc := wndClassEx{Style: c.style, WndProc: c.proc, Cursor: arrow, ClassName: n}
			wc.Size = uint32(unsafe.Sizeof(wc))
			if r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
				classesErr = errors.New("注册窗口类失败")
				return
			}
		}
	})
	return classesErr
}

// findDesktop returns the window holding the desktop's icons and the icon
// list itself. The holder is a child of Progman, or of a WorkerW window
// when a wallpaper program split the desktop.
func findDesktop() (defView, folderView uintptr) {
	defClass, _ := windows.UTF16PtrFromString("SHELLDLL_DefView")
	listClass, _ := windows.UTF16PtrFromString("SysListView32")
	progClass, _ := windows.UTF16PtrFromString("Progman")
	workerClass, _ := windows.UTF16PtrFromString("WorkerW")
	progman, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(progClass)), 0)
	defView, _, _ = procFindWindowExW.Call(progman, 0, uintptr(unsafe.Pointer(defClass)), 0)
	for w := uintptr(0); defView == 0; {
		w, _, _ = procFindWindowExW.Call(0, w, uintptr(unsafe.Pointer(workerClass)), 0)
		if w == 0 {
			return 0, 0
		}
		defView, _, _ = procFindWindowExW.Call(w, 0, uintptr(unsafe.Pointer(defClass)), 0)
	}
	folderView, _, _ = procFindWindowExW.Call(defView, 0, uintptr(unsafe.Pointer(listClass)), 0)
	return defView, folderView
}

// attach finds the desktop, hides its icons and lays the surfaces over it.
func (d *Desktop) attach() error {
	defView, folderView := findDesktop()
	if defView == 0 {
		return errors.New("找不到桌面窗口")
	}
	monitors := desktopMonitors()
	if len(monitors) == 0 {
		return errors.New("找不到显示器")
	}
	cls, _ := windows.UTF16PtrFromString(surfaceClass)
	var surfaces []*surface
	for _, m := range monitors {
		o := point{X: int32(m.Bounds.X), Y: int32(m.Bounds.Y)}
		procScreenToClient.Call(defView, uintptr(unsafe.Pointer(&o)))
		w, h := int(m.Bounds.Width), int(m.Bounds.Height)
		hwnd, _, _ := procCreateWindowExW.Call(wsExLayered, uintptr(unsafe.Pointer(cls)), 0, wsChild|wsVisible|wsClipSiblings,
			uintptr(o.X), uintptr(o.Y), uintptr(w), uintptr(h), defView, 0, 0, 0)
		if hwnd == 0 {
			destroySurfaces(surfaces)
			return errors.New("创建桌面图层失败")
		}
		procSetWindowPos.Call(hwnd, hwndTop, 0, 0, 0, 0, 0x0001|0x0002|swpNoActivate) // keep size and position
		canvas, err := newCanvas(w, h, m.Bounds.X, m.Bounds.Y)
		if err != nil {
			procDestroyWindow.Call(hwnd)
			destroySurfaces(surfaces)
			return err
		}
		s := &surface{hwnd: hwnd, monitor: m, canvas: canvas}
		s.drop = newDropTarget(d, s)
		procRegisterDragDrop.Call(hwnd, uintptr(unsafe.Pointer(s.drop)))
		surfaceOf.Store(hwnd, surfaceRef{d, s})
		surfaces = append(surfaces, s)
	}
	if folderView != 0 {
		if d.explorer == nil {
			// Without Explorer's positions its icons are left where it
			// puts them; the takeover goes on all the same.
			d.explorer, _ = readIconLayout(folderView)
		}
		procShowWindow.Call(folderView, swHide)
	}
	d.defView, d.folderView, d.surfaces = defView, folderView, surfaces
	d.handler.Monitors(monitors)
	d.invalidateAll()
	return nil
}

type surfaceRef struct {
	d *Desktop
	s *surface
}

// surfaceOf finds a surface by its window.
var surfaceOf sync.Map

func destroySurfaces(surfaces []*surface) {
	for _, s := range surfaces {
		procRevokeDragDrop.Call(s.hwnd)
		surfaceOf.Delete(s.hwnd)
		procDestroyWindow.Call(s.hwnd)
		s.canvas.release()
	}
}

// detach removes the surfaces and shows File Explorer's icons again.
func (d *Desktop) detach() {
	d.detaching = true
	d.endEdit(false)
	destroySurfaces(d.surfaces)
	d.surfaces = nil
	if d.folderView != 0 && isWindow(d.folderView) {
		procShowWindow.Call(d.folderView, swShow)
	}
	d.detaching = false
}

// reattach lays the surfaces again, after the monitors changed or File
// Explorer restarted. Until the desktop is back, it tries again shortly.
func (d *Desktop) reattach() {
	d.detach()
	if err := d.attach(); err != nil {
		procSetTimer.Call(d.host, timerReattach, 500, 0)
	}
}

func isWindow(h uintptr) bool {
	r, _, _ := procIsWindow.Call(h)
	return r != 0
}

func desktopMonitors() []DesktopMonitor {
	monitorsFound = nil
	procEnumDisplayMonitors.Call(0, 0, monitorCallback, 0)
	out := monitorsFound
	monitorsFound = nil
	return out
}

var (
	monitorsFound   []DesktopMonitor
	monitorCallback = windows.NewCallback(func(hmon, _, _, _ uintptr) uintptr {
		var info struct {
			Size    uint32
			Monitor rect
			Work    rect
			Flags   uint32
			Device  [32]uint16
		}
		info.Size = uint32(unsafe.Sizeof(info))
		if r, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&info))); r == 0 {
			return 1
		}
		var dpiX, dpiY uint32
		scale := 1.0
		if hr, _, _ := procGetDpiForMonitor.Call(hmon, mdtEffectiveDpi, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY))); succeeded(hr) && dpiX > 0 {
			scale = float64(dpiX) / 96
		}
		box := func(r rect) Box {
			return Box{X: float64(r.Left), Y: float64(r.Top), Width: float64(r.Right - r.Left), Height: float64(r.Bottom - r.Top)}
		}
		m := DesktopMonitor{ID: windows.UTF16ToString(info.Device[:]), Bounds: box(info.Monitor), Work: box(info.Work), Scale: scale}
		// The primary monitor comes first.
		if info.Flags&1 != 0 {
			monitorsFound = append([]DesktopMonitor{m}, monitorsFound...)
		} else {
			monitorsFound = append(monitorsFound, m)
		}
		return 1
	})
)

// Invalidate redraws the part of the screen inside b.
func (d *Desktop) Invalidate(b Box) {
	for _, s := range d.surfaces {
		if r, ok := intersect(b, s.monitor.Bounds); ok {
			s.dirty = union(s.dirty, r)
		}
	}
	if !d.paintSent {
		d.paintSent = true
		procPostMessageW.Call(d.host, wmPaintLayer, 0, 0)
	}
}

func (d *Desktop) invalidateAll() {
	for _, s := range d.surfaces {
		d.Invalidate(s.monitor.Bounds)
	}
}

func intersect(a, b Box) (Box, bool) {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if x1 <= x0 || y1 <= y0 {
		return Box{}, false
	}
	return Box{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}

func union(a, b Box) Box {
	if a.Width <= 0 || a.Height <= 0 {
		return b
	}
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.Width, b.X+b.Width), max(a.Y+a.Height, b.Y+b.Height)
	return Box{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

func (d *Desktop) paint() {
	d.paintSent = false
	for _, s := range d.surfaces {
		if s.dirty.Width <= 0 {
			continue
		}
		dirty := s.dirty
		if !s.full {
			dirty = s.monitor.Bounds
		}
		// Whole pixels, so the cleared area and the copied area agree.
		dirty = snapOut(dirty)
		s.dirty = Box{}
		for range 2 {
			s.canvas.begin(dirty)
			d.handler.Paint(s.canvas, dirty)
			redraw, err := s.canvas.end()
			if err != nil {
				return
			}
			if !redraw {
				break
			}
			dirty = s.monitor.Bounds
		}
		d.present(s, &rect{
			Left: int32(dirty.X - s.monitor.Bounds.X), Top: int32(dirty.Y - s.monitor.Bounds.Y),
			Right: int32(dirty.X + dirty.Width - s.monitor.Bounds.X), Bottom: int32(dirty.Y + dirty.Height - s.monitor.Bounds.Y),
		})
		s.full = true
	}
}

func snapOut(b Box) Box {
	x0, y0 := float64(int(b.X)), float64(int(b.Y))
	if x0 > b.X {
		x0--
	}
	if y0 > b.Y {
		y0--
	}
	x1, y1 := float64(int(b.X+b.Width)), float64(int(b.Y+b.Height))
	if x1 < b.X+b.Width {
		x1++
	}
	if y1 < b.Y+b.Height {
		y1++
	}
	return Box{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// present copies the canvas to the window; dirty limits it, nil copies all.
func (d *Desktop) present(s *surface, dirty *rect) {
	size := struct{ CX, CY int32 }{int32(s.canvas.width), int32(s.canvas.height)}
	src := point{}
	blend := [4]byte{0, 0, d.alpha, acSrcAlpha}
	info := struct {
		Size  uint32
		Dst   uintptr
		PtDst uintptr
		PSize uintptr
		Src   uintptr
		PtSrc uintptr
		Key   uint32
		Blend uintptr
		Flags uint32
		Dirty uintptr
	}{
		PSize: uintptr(unsafe.Pointer(&size)), Src: s.canvas.dc, PtSrc: uintptr(unsafe.Pointer(&src)),
		Blend: uintptr(unsafe.Pointer(&blend)), Flags: ulwAlpha, Dirty: uintptr(unsafe.Pointer(dirty)),
	}
	info.Size = uint32(unsafe.Sizeof(info))
	procUpdateLayeredWindowIndir.Call(s.hwnd, uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(dirty)
}

// SetAlpha fades the whole layer, without drawing it again.
func (d *Desktop) SetAlpha(a byte) {
	d.alpha = a
	for _, s := range d.surfaces {
		if s.full {
			d.present(s, nil)
		}
	}
}

// Animate turns the frame ticks on or off.
func (d *Desktop) Animate(on bool) {
	if on == d.animating {
		return
	}
	d.animating = on
	if on {
		procSetTimer.Call(d.host, timerAnimate, frameMS, 0)
	} else {
		procKillTimer.Call(d.host, timerAnimate)
	}
}

// SetCursor sets the mouse pointer shown over the layer.
func (d *Desktop) SetCursor(c Cursor) {
	if c == d.cursor {
		return
	}
	d.cursor = c
	h, _, _ := procLoadCursorW.Call(0, uintptr(c))
	procSetCursor.Call(h)
}

// Capture keeps the mouse with the layer while a button is held, so a drag
// can leave it.
func (d *Desktop) Capture(x, y float64, on bool) {
	if !on {
		procReleaseCapture.Call()
		return
	}
	if s := d.surfaceAt(x, y); s != nil {
		procSetCapture.Call(s.hwnd)
	}
}

// Focus gives the layer the keyboard.
func (d *Desktop) Focus(x, y float64) {
	if s := d.surfaceAt(x, y); s != nil {
		procSetFocus.Call(s.hwnd)
	}
}

func (d *Desktop) surfaceAt(x, y float64) *surface {
	for _, s := range d.surfaces {
		b := s.monitor.Bounds
		if x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height {
			return s
		}
	}
	if len(d.surfaces) > 0 {
		return d.surfaces[0]
	}
	return nil
}

// Menu shows a menu at a screen point and returns the ID of the entry
// chosen, or 0.
func (d *Desktop) Menu(items []MenuItem, x, y float64) int {
	s := d.surfaceAt(x, y)
	if s == nil {
		return 0
	}
	m := buildMenu(items)
	defer procDestroyMenu.Call(m)
	return d.track(s, m, x, y)
}

func (d *Desktop) track(s *surface, m uintptr, x, y float64) int {
	d.handler.Pointer(PointerEvent{Kind: PointerCancel, X: x, Y: y})
	procSetForegroundWindow.Call(s.hwnd)
	r, _, _ := procTrackPopupMenuEx.Call(m, tpmReturnCmd|tpmRightButton, uintptr(int32(x)), uintptr(int32(y)), s.hwnd, 0)
	return int(r)
}

func buildMenu(items []MenuItem) uintptr {
	m, _, _ := procCreatePopupMenu.Call()
	appendMenu(m, items)
	return m
}

func appendMenu(m uintptr, items []MenuItem) {
	for _, it := range items {
		if it.Separator {
			procAppendMenuW.Call(m, mfSeparator, 0, 0)
			continue
		}
		text, _ := windows.UTF16PtrFromString(it.Text)
		flags, id := uintptr(mfString), uintptr(it.ID)
		if it.Checked {
			flags |= mfChecked
		}
		if it.Disabled {
			flags |= mfGrayed
		}
		if len(it.Sub) > 0 {
			sub, _, _ := procCreatePopupMenu.Call()
			appendMenu(sub, it.Sub)
			flags, id = flags|mfPopup, sub
		}
		procAppendMenuW.Call(m, flags, id, uintptr(unsafe.Pointer(text)))
	}
}

// editBox is the box for editing a name in place: a popup window holding
// an edit control, since a layered window cannot show child controls.
type editBox struct {
	frame, edit uintptr
	font        uintptr
	oldProc     uintptr
	finished    bool
}

// Edit shows a box for editing text over b, with the text up to selectTo
// characters selected. EditDone reports the result.
func (d *Desktop) Edit(b Box, text string, selectTo int, f Font) {
	d.endEdit(false)
	cls, _ := windows.UTF16PtrFromString(editClass)
	frame, _, _ := procCreateWindowExW.Call(wsExToolWindow|wsExTopmost, uintptr(unsafe.Pointer(cls)), 0, wsPopup|wsBorder,
		uintptr(int32(math.Round(b.X))), uintptr(int32(math.Round(b.Y))), uintptr(int32(math.Round(b.Width))), uintptr(int32(math.Round(b.Height))), d.host, 0, 0, 0)
	if frame == 0 {
		d.handler.EditDone(text, false)
		return
	}
	family, _ := windows.UTF16PtrFromString(f.Family)
	font, _, _ := procCreateFontW.Call(uintptr(-int32(f.Size)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(family)))
	// A one-line box is a single-line edit as tall as its text, in the
	// middle of the frame, since an edit draws its text at its top.
	inner := int32(math.Round(b.Height)) - 2
	style, y, h := uintptr(wsChild|wsVisible|esCenter|esMultiline|esAutoVScroll), int32(0), inner
	if f.Lines == 1 {
		style = wsChild | wsVisible | esCenter | esAutoHScroll
		h = min(inner, textHeight(font))
		y = (inner - h) / 2
	}
	editCls, _ := windows.UTF16PtrFromString("EDIT")
	value, _ := windows.UTF16PtrFromString(text)
	edit, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(editCls)), uintptr(unsafe.Pointer(value)),
		style, 0, uintptr(y), uintptr(int32(b.Width-2)), uintptr(h), frame, 0, 0, 0)
	procSendMessageW.Call(edit, wmSetFont, font, 1)
	e := &editBox{frame: frame, edit: edit, font: font}
	e.oldProc, _, _ = procSetWindowLongPtrW.Call(edit, gwlpWndProc, editProc)
	d.edit = e
	edits.Store(edit, d)
	procShowWindow.Call(frame, swShow)
	procSetForegroundWindow.Call(frame)
	procSetFocus.Call(edit)
	procSendMessageW.Call(edit, emSetSel, 0, uintptr(selectTo))
}

var edits sync.Map

// textHeight is the height of a line of text in a font, in pixels.
func textHeight(font uintptr) int32 {
	dc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, dc)
	old, _, _ := procSelectObject.Call(dc, font)
	defer procSelectObject.Call(dc, old)
	var tm [64]byte // TEXTMETRICW; tmHeight comes first
	procGetTextMetricsW.Call(dc, uintptr(unsafe.Pointer(&tm[0])))
	return int32(binary.LittleEndian.Uint32(tm[:4]))
}

// endEdit closes the edit box, reporting its text when ok.
func (d *Desktop) endEdit(ok bool) {
	e := d.edit
	if e == nil || e.finished {
		return
	}
	e.finished = true
	n, _, _ := procGetWindowTextLengthW.Call(e.edit)
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(e.edit, uintptr(unsafe.Pointer(&buf[0])), n+1)
	text := windows.UTF16ToString(buf)
	procSetWindowLongPtrW.Call(e.edit, gwlpWndProc, e.oldProc)
	edits.Delete(e.edit)
	d.edit = nil
	procDestroyWindow.Call(e.frame)
	procDeleteObject.Call(e.font)
	d.handler.EditDone(text, ok)
}

func editWndProc(hwnd, m, w, l uintptr) uintptr {
	v, ok := edits.Load(hwnd)
	if !ok {
		r, _, _ := procDefWindowProcW.Call(hwnd, m, w, l)
		return r
	}
	d := v.(*Desktop)
	old := d.edit.oldProc
	switch m {
	case wmKeyDown:
		switch w {
		case vkReturn:
			d.endEdit(true)
			return 0
		case vkEscape:
			d.endEdit(false)
			return 0
		}
	case wmChar:
		if w == '\r' || w == '\n' || w == vkEscape {
			return 0
		}
	case wmKillFocus:
		r, _, _ := procCallWindowProcW.Call(old, hwnd, m, w, l)
		d.endEdit(true)
		return r
	}
	r, _, _ := procCallWindowProcW.Call(old, hwnd, m, w, l)
	return r
}

func hostWndProc(hwnd, m, w, l uintptr) uintptr {
	v, ok := desktops.Load(hwnd)
	if !ok {
		r, _, _ := procDefWindowProcW.Call(hwnd, m, w, l)
		return r
	}
	d := v.(*Desktop)
	switch {
	case m == wmRun:
		d.mu.Lock()
		queue := d.queue
		d.queue = nil
		d.mu.Unlock()
		for _, f := range queue {
			f()
		}
		return 0
	case m == wmPaintLayer:
		d.paint()
		return 0
	case m == wmShellNotify:
		d.shellNotified(w, l)
		return 0
	case m == wmTimer && w == timerAnimate:
		d.handler.Tick()
		return 0
	case m == wmTimer && w == timerReattach:
		procKillTimer.Call(hwnd, timerReattach)
		d.reattach()
		return 0
	case m == taskbarCreated && taskbarCreated != 0, m == wmDisplayChange:
		d.reattach()
		return 0
	case m == wmSettingChange && w == 0x2F: // SPI_SETWORKAREA
		d.reattach()
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, w, l)
	return r
}

func modifiers() (ctrl, shift, alt bool) {
	down := func(vk uintptr) bool {
		r, _, _ := procGetKeyState.Call(vk)
		return int16(r) < 0
	}
	return down(vkControl), down(vkShift), down(vkMenu)
}

func surfaceWndProc(hwnd, m, w, l uintptr) uintptr {
	v, ok := surfaceOf.Load(hwnd)
	if !ok {
		r, _, _ := procDefWindowProcW.Call(hwnd, m, w, l)
		return r
	}
	ref := v.(surfaceRef)
	d, s := ref.d, ref.s
	if d.menu != nil {
		switch m {
		case wmInitMenuPopup, wmDrawItem, wmMeasureItem, wmMenuChar, wmMenuSelect:
			if r, handled := handleMenuMessage(d.menu, m, w, l); handled {
				return r
			}
		}
	}
	at := func() (float64, float64) {
		return s.monitor.Bounds.X + float64(int16(l&0xffff)), s.monitor.Bounds.Y + float64(int16(l>>16&0xffff))
	}
	pointer := func(kind PointerKind, button int) uintptr {
		x, y := at()
		ctrl, shift, alt := modifiers()
		d.handler.Pointer(PointerEvent{Kind: kind, Button: button, X: x, Y: y, Ctrl: ctrl, Shift: shift, Alt: alt})
		return 0
	}
	switch m {
	case wmMouseMove:
		if d.tracking != hwnd {
			t := struct {
				Size, Flags uint32
				Hwnd        uintptr
				Hover       uint32
			}{Flags: tmeLeave, Hwnd: hwnd}
			t.Size = uint32(unsafe.Sizeof(t))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
			d.tracking = hwnd
		}
		return pointer(PointerMove, 0)
	case wmMouseLeave:
		d.tracking = 0
		return pointer(PointerLeave, 0)
	case wmLButtonDown:
		procSetFocus.Call(hwnd)
		return pointer(PointerDown, 1)
	case wmLButtonUp:
		return pointer(PointerUp, 1)
	case wmLButtonDblClk:
		return pointer(PointerDoubleClick, 1)
	case wmRButtonDown:
		procSetFocus.Call(hwnd)
		return pointer(PointerDown, 2)
	case wmRButtonUp:
		return pointer(PointerUp, 2)
	case wmMButtonDown:
		return pointer(PointerDown, 3)
	case wmCaptureChanged:
		if w != hwnd {
			return pointer(PointerCancel, 0)
		}
		return 0
	case wmMouseWheel:
		ctrl, shift, alt := modifiers()
		d.handler.Pointer(PointerEvent{
			Kind: PointerWheel, X: float64(int16(l & 0xffff)), Y: float64(int16(l >> 16 & 0xffff)),
			Wheel: float64(int16(w>>16&0xffff)) / 120, Ctrl: ctrl, Shift: shift, Alt: alt,
		})
		return 0
	case wmSetCursor:
		if l&0xffff == htClient {
			h, _, _ := procLoadCursorW.Call(0, uintptr(d.cursor))
			procSetCursor.Call(h)
			return 1
		}
	case wmKeyDown, wmSysKeyDown:
		ctrl, shift, alt := modifiers()
		d.handler.Key(KeyEvent{Key: uint32(w), Ctrl: ctrl, Shift: shift, Alt: alt})
		return 0
	case wmChar:
		return 0
	case wmDestroy:
		// File Explorer closed the desktop under the layer, as when it
		// restarts; lay it again once the desktop is back.
		if !d.detaching {
			surfaceOf.Delete(hwnd)
			procSetTimer.Call(d.host, timerReattach, 500, 0)
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, w, l)
	return r
}
