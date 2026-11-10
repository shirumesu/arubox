package platform

import (
	"errors"
	"runtime"
	"strconv"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	whKeyboardLL  = 13
	wmKeyDown     = 0x0100
	wmSysKeyDown  = 0x0104
	wmQuit        = 0x0012
	llkhfInjected = 0x10
	sizeofMsg     = 48
)

type kbdllHookStruct struct {
	vkCode, scanCode, flags, time uint32
	extraInfo                     uintptr
}

// keyNames maps virtual-key codes to the names hotkeys are registered by.
var keyNames = func() map[uint32]string {
	names := map[uint32]string{
		0xBA: ";", 0xBB: "=", 0xBC: ",", 0xBD: "-", 0xBE: ".", 0xBF: "/",
		0xC0: "`", 0xDB: "[", 0xDC: "\\", 0xDD: "]", 0xDE: "'",
		0x08: "backspace", 0x09: "tab", 0x0D: "return", 0x1B: "escape",
		0x20: "space", 0x21: "page up", 0x22: "page down", 0x23: "end",
		0x24: "home", 0x25: "left", 0x26: "up", 0x27: "right", 0x28: "down",
		0x2E: "delete",
	}
	for c := 'a'; c <= 'z'; c++ {
		names[uint32(c-'a'+'A')] = string(c)
	}
	for c := '0'; c <= '9'; c++ {
		names[uint32(c)] = string(c)
	}
	for i := 1; i <= 20; i++ {
		names[uint32(0x6F+i)] = "f" + strconv.Itoa(i)
	}
	return names
}()

var modifierKeys = map[uint32]string{
	0x10: "Shift", 0xA0: "Shift", 0xA1: "Shift",
	0x11: "Ctrl", 0xA2: "Ctrl", 0xA3: "Ctrl",
	0x12: "Alt", 0xA4: "Alt", 0xA5: "Alt",
	0x5B: "Super", 0x5C: "Super",
}

var modifierOrder = []string{"Ctrl", "Alt", "Shift", "Super"}

// A Go callback cannot be freed, so the hook is made once and works on
// keyCapture, which only the hook's thread touches while it is installed.
var (
	keyCapture struct {
		presses chan<- KeyPress
		held    map[uint32]bool
	}
	keyHook = windows.NewCallback(hookKey)
)

// CaptureKeys takes every key pressed while a window of arubox is in front
// and passes it to on instead, before Windows, the shell or other programs
// can act on it: Alt+Space does not open the window menu and Win+E does not
// open Explorer. Keys pressed in other programs pass through. Only one
// capture may run at a time; stop ends it.
func CaptureKeys(on func(KeyPress)) (stop func(), err error) {
	presses := make(chan KeyPress, 64)
	ready := make(chan error)
	done := make(chan struct{})
	var thread uint32
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		defer close(presses)
		// Make the thread's message queue now, so stop can post to it at once.
		var msg [sizeofMsg]byte
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0)
		thread = windows.GetCurrentThreadId()
		keyCapture.presses, keyCapture.held = presses, map[uint32]bool{}
		module, _, _ := procGetModuleHandleW.Call(0)
		hook, _, callErr := procSetWindowsHookExW.Call(whKeyboardLL, keyHook, module, 0)
		if hook == 0 {
			ready <- errors.Join(errors.New("install keyboard hook"), callErr)
			return
		}
		defer procUnhookWindowsHookEx.Call(hook)
		ready <- nil
		// The hook runs on this thread while it waits for messages.
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0); int32(r) <= 0 {
				return
			}
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	go func() {
		for p := range presses {
			on(p)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			procPostThreadMessageW.Call(uintptr(thread), wmQuit, 0, 0)
			<-done
		})
	}, nil
}

func hookKey(code, wParam, lParam uintptr) uintptr {
	if int32(code) < 0 {
		return callNextHook(code, wParam, lParam)
	}
	info := (*kbdllHookStruct)(unsafe.Pointer(lParam)) //nolint:govet // owned by Windows, outside the Go heap
	// Keys arubox types itself, such as the copy for selection translation,
	// pass through.
	if info.flags&llkhfInjected != 0 || !ownForeground() {
		return callNextHook(code, wParam, lParam)
	}
	held := keyCapture.held
	down := wParam == wmKeyDown || wParam == wmSysKeyDown
	if down {
		held[info.vkCode] = true
	} else {
		delete(held, info.vkCode)
	}
	mods := map[string]bool{}
	for vk := range held {
		if m, ok := modifierKeys[vk]; ok {
			mods[m] = true
		}
	}
	press := KeyPress{Key: keyNames[info.vkCode], Modifiers: []string{}, Down: down, Idle: len(held) == 0}
	for _, m := range modifierOrder {
		if mods[m] {
			press.Modifiers = append(press.Modifiers, m)
		}
	}
	keyCapture.presses <- press
	// Swallowed: nothing else sees the key.
	return 1
}

func callNextHook(code, wParam, lParam uintptr) uintptr {
	r, _, _ := procCallNextHookEx.Call(0, code, wParam, lParam)
	return r
}

func ownForeground() bool {
	var pid uint32
	_, _ = windows.GetWindowThreadProcessId(windows.GetForegroundWindow(), &pid)
	return pid == windows.GetCurrentProcessId()
}
