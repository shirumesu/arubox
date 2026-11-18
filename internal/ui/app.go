package ui

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The UI thread is the one Run locks. Every window, the tray icon and the
// hotkeys belong to it; code on other goroutines reaches it with Post or
// Sync.

const (
	mainClass = "arubox.main"
	// msgInvoke asks the main window to run the posted functions.
	msgInvoke = wmApp + 1
	// msgTray is the tray icon's callback message.
	msgTray = wmApp + 2
)

var (
	uiThread  uint32
	mainHwnd  uintptr
	instance  uintptr
	wndProcCB = windows.NewCallback(wndProc)

	queueMu sync.Mutex
	queue   []func()

	// windows maps each window handle to what handles its messages.
	handlers = map[uintptr]messageHandler{}

	hotkeyMu   sync.Mutex
	hotkeyNext = 1
	hotkeyFns  = map[int]func(){}

	themeMu       sync.Mutex
	themeWatchers []func(dark bool)
	darkNow       bool

	secondInstance func()
	msgSecond      uint32
	msgTaskbarMade uint32
	quitHooks      []func()
)

// messageHandler is a window that handles its own messages. It returns
// handled false to fall back to the default handling.
type messageHandler interface {
	handle(hwnd uintptr, m uint32, wp, lp uintptr) (result uintptr, handled bool)
}

// Options configures Run.
type Options struct {
	// ID names the program for single-instance detection.
	ID string
	// OnSecondInstance runs on the UI thread when the program is started
	// again while running; the second copy then exits.
	OnSecondInstance func()
}

// ErrRunning is returned by Run when another copy of the program is
// already running; that copy was told and the caller should exit.
var ErrRunning = errors.New("ui: already running")

// Run starts the UI thread, calls start on it and handles messages until
// Quit. It returns ErrRunning when another copy already runs.
func Run(opts Options, start func()) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	procSetProcessDpiAwarenessContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	procCoInitializeEx.Call(0, 0x2)                     // COINIT_APARTMENTTHREADED
	allowDarkMenus()

	msgSecond = registerMessage("arubox.second-instance." + opts.ID)
	msgTaskbarMade = registerMessage("TaskbarCreated")
	if opts.ID != "" {
		name := utf16Ptr("Local\\arubox.single." + opts.ID)
		h, _, err := procCreateMutexW.Call(0, 0, name)
		if h == 0 {
			return err
		}
		if errors.Is(err, windows.Errno(errorAlreadyExists)) {
			if other, _, _ := procFindWindowW.Call(utf16Ptr(mainClass), 0); other != 0 {
				procPostMessageW.Call(other, uintptr(msgSecond), 0, 0)
			}
			return ErrRunning
		}
		// The handle stays open for the life of the process.
	}
	secondInstance = opts.OnSecondInstance

	uiThread = currentThread()
	instance = moduleHandle()
	if err := registerClass(mainClass, 0, 0); err != nil {
		return err
	}
	// A hidden top-level window, not a message-only one: only top-level
	// windows hear broadcasts such as a theme change or Explorer
	// restarting.
	h, _, err := procCreateWindowExW.Call(wsExToolWindow, utf16Ptr(mainClass), utf16Ptr("arubox"), wsPopup, 0, 0, 0, 0, 0, 0, instance, 0)
	if h == 0 {
		return err
	}
	mainHwnd = h
	handlers[h] = mainWindow{}
	darkNow = readDarkMode()

	start()

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if dialogKey(&m) {
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	for _, f := range quitHooks {
		f()
	}
	return nil
}

// allowDarkMenus lets native menus follow the dark theme, through the
// undocumented uxtheme exports File Explorer and Notepad use (ordinals
// 135 SetPreferredAppMode and 136 FlushMenuThemes, Windows 10 1903 on).
func allowDarkMenus() {
	h, err := windows.LoadLibrary("uxtheme.dll")
	if err != nil {
		return
	}
	if p, err := windows.GetProcAddressByOrdinal(h, 135); err == nil {
		_, _, _ = syscall.SyscallN(p, 1) // AllowDark
	}
	if p, err := windows.GetProcAddressByOrdinal(h, 136); err == nil {
		_, _, _ = syscall.SyscallN(p)
	}
}

// Quit ends Run once the current message is handled.
func Quit() {
	Post(func() { procPostQuitMessage.Call(0) })
}

// OnQuit registers f to run on the UI thread as Run returns.
func OnQuit(f func()) {
	Post(func() { quitHooks = append(quitHooks, f) })
}

// OnUIThread reports whether the caller runs on the UI thread.
func OnUIThread() bool { return currentThread() == uiThread }

// Post runs f on the UI thread later. It is safe from any goroutine.
func Post(f func()) {
	queueMu.Lock()
	queue = append(queue, f)
	first := len(queue) == 1
	queueMu.Unlock()
	if first {
		procPostMessageW.Call(mainHwnd, msgInvoke, 0, 0)
	}
}

// Sync runs f on the UI thread and waits for it. On the UI thread it runs
// f at once.
func Sync(f func()) {
	if OnUIThread() {
		f()
		return
	}
	done := make(chan struct{})
	Post(func() {
		defer close(done)
		f()
	})
	<-done
}

func runQueue() {
	queueMu.Lock()
	q := queue
	queue = nil
	queueMu.Unlock()
	for _, f := range q {
		f()
	}
}

// DarkMode reports whether apps should use the dark theme.
func DarkMode() bool {
	themeMu.Lock()
	defer themeMu.Unlock()
	return darkNow
}

// OnThemeChange calls f on the UI thread whenever the system switches
// between light and dark.
func OnThemeChange(f func(dark bool)) {
	themeMu.Lock()
	themeWatchers = append(themeWatchers, f)
	themeMu.Unlock()
}

func readDarkMode() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

type mainWindow struct{}

func (mainWindow) handle(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case msgInvoke:
		runQueue()
		return 0, true
	case wmHotkey:
		hotkeyMu.Lock()
		f := hotkeyFns[int(wp)]
		hotkeyMu.Unlock()
		if f != nil {
			go f()
		}
		return 0, true
	case msgTray:
		trayMessage(wp, lp)
		return 0, true
	case wmClose:
		// taskkill and installers close a program by its windows; quit
		// properly, so modules give back what they took.
		procPostQuitMessage.Call(0)
		return 0, true
	case 0x0016: // WM_ENDSESSION
		if wp != 0 {
			// Windows is signing out: the process ends once this returns,
			// before the message loop would run the hooks.
			for _, f := range quitHooks {
				f()
			}
			quitHooks = nil
		}
		return 0, true
	case wmSettingChange:
		if lp != 0 && windows.UTF16PtrToString((*uint16)(unsafe.Pointer(lp))) == "ImmersiveColorSet" {
			dark := readDarkMode()
			themeMu.Lock()
			changed := dark != darkNow
			darkNow = dark
			ws := append([]func(bool){}, themeWatchers...)
			themeMu.Unlock()
			if changed {
				for _, f := range ws {
					f(dark)
				}
			}
		}
		return 0, false
	}
	if m == msgSecond && msgSecond != 0 {
		if secondInstance != nil {
			secondInstance()
		}
		return 0, true
	}
	if m == msgTaskbarMade && msgTaskbarMade != 0 {
		trayRecreate()
		return 0, true
	}
	return 0, false
}

func registerMessage(name string) uint32 {
	r, _, _ := procRegisterWindowMessageW.Call(utf16Ptr(name))
	return uint32(r)
}

var registered = map[string]bool{}

func registerClass(name string, style uint32, cursor int) error {
	if registered[name] {
		return nil
	}
	var cur uintptr
	if cursor != 0 {
		cur, _, _ = procLoadCursorW.Call(0, uintptr(cursor))
	}
	wc := wndClassEx{
		Style:     style,
		WndProc:   wndProcCB,
		Instance:  instance,
		Cursor:    cur,
		ClassName: utf16(name),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	registered[name] = true
	return nil
}

func wndProc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	if h := handlers[hwnd]; h != nil {
		if r, ok := h.handle(hwnd, m, wp, lp); ok {
			return r
		}
	} else if pending != nil && m != wmDestroy {
		// Messages sent while CreateWindowEx runs arrive before the handle
		// is known.
		handlers[hwnd] = pending
		pending = nil
		if r, ok := handlers[hwnd].handle(hwnd, m, wp, lp); ok {
			return r
		}
	}
	if m == wmDestroy {
		delete(handlers, hwnd)
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), wp, lp)
	return r
}

// pending is the window being created, until its handle is known.
var pending messageHandler

func createWindow(h messageHandler, exStyle, style uint32, class, title string, x, y, w, ht int, parent uintptr) (uintptr, error) {
	pending = h
	hwnd, _, err := procCreateWindowExW.Call(uintptr(exStyle), utf16Ptr(class), utf16Ptr(title), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(ht), parent, 0, instance, 0)
	pending = nil
	if hwnd == 0 {
		return 0, err
	}
	handlers[hwnd] = h
	return hwnd, nil
}

// Hotkey modifiers.
const (
	ModAlt   = modAlt
	ModCtrl  = modControl
	ModShift = modShift
	ModWin   = modWin
)

// ErrHotkeyTaken is returned when the system refuses a hotkey.
var ErrHotkeyTaken = errors.New("ui: hotkey taken")

// RegisterHotkey binds a global hotkey; f runs on its own goroutine each
// time it is pressed. It returns an id for UnregisterHotkey.
func RegisterHotkey(mods, vk uint32, f func()) (int, error) {
	hotkeyMu.Lock()
	id := hotkeyNext
	hotkeyNext++
	hotkeyMu.Unlock()
	var ok bool
	Sync(func() {
		r, _, _ := procRegisterHotKey.Call(mainHwnd, uintptr(id), uintptr(mods|modNoRepeat), uintptr(vk))
		ok = r != 0
	})
	if !ok {
		return 0, ErrHotkeyTaken
	}
	hotkeyMu.Lock()
	hotkeyFns[id] = f
	hotkeyMu.Unlock()
	return id, nil
}

// UnregisterHotkey removes a hotkey RegisterHotkey bound.
func UnregisterHotkey(id int) {
	hotkeyMu.Lock()
	delete(hotkeyFns, id)
	hotkeyMu.Unlock()
	Sync(func() { procUnregisterHotKey.Call(mainHwnd, uintptr(id)) })
}

// OpenURL opens a web address or a file with its default program.
func OpenURL(target string) error {
	r, _, err := procShellExecuteW.Call(0, utf16Ptr("open"), utf16Ptr(target), 0, 0, swShowNormal)
	if r <= 32 {
		return err
	}
	return nil
}

// Beep plays the system's default sound.
func Beep() { procMessageBeep.Call(0) }
