// Package ui is arubox's user interface: native Win32 windows drawn with
// Direct2D and DirectWrite, a tray icon, global hotkeys and the UI thread
// they all run on. It holds no WebView and no GPU device: every window is
// drawn in software, which keeps the resident memory of the program small.
package ui

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	uxtheme  = windows.NewLazySystemDLL("uxtheme.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procSendMessageW                  = user32.NewProc("SendMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procInvalidateRect                = user32.NewProc("InvalidateRect")
	procValidateRect                  = user32.NewProc("ValidateRect")
	procBeginPaint                    = user32.NewProc("BeginPaint")
	procEndPaint                      = user32.NewProc("EndPaint")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procSetFocus                      = user32.NewProc("SetFocus")
	procGetFocus                      = user32.NewProc("GetFocus")
	procSetCapture                    = user32.NewProc("SetCapture")
	procReleaseCapture                = user32.NewProc("ReleaseCapture")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procSetWindowRgn                  = user32.NewProc("SetWindowRgn")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procSetCursor                     = user32.NewProc("SetCursor")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procScreenToClient                = user32.NewProc("ScreenToClient")
	procClientToScreen                = user32.NewProc("ClientToScreen")
	procTrackMouseEvent               = user32.NewProc("TrackMouseEvent")
	procGetKeyState                   = user32.NewProc("GetKeyState")
	procSetTimer                      = user32.NewProc("SetTimer")
	procKillTimer                     = user32.NewProc("KillTimer")
	procRegisterHotKey                = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey              = user32.NewProc("UnregisterHotKey")
	procRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	procFindWindowW                   = user32.NewProc("FindWindowW")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procMonitorFromPoint              = user32.NewProc("MonitorFromPoint")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                   = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx              = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu                   = user32.NewProc("DestroyMenu")
	procSetWindowTextW                = user32.NewProc("SetWindowTextW")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW          = user32.NewProc("GetWindowTextLengthW")
	procCreateIconFromResourceEx      = user32.NewProc("CreateIconFromResourceEx")
	procMessageBeep                   = user32.NewProc("MessageBeep")
	procGetParent                     = user32.NewProc("GetParent")

	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW       = kernel32.NewProc("CreateMutexW")

	procCreateFontW           = gdi32.NewProc("CreateFontW")
	procDeleteObject          = gdi32.NewProc("DeleteObject")
	procCreateCompatibleDC    = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC              = gdi32.NewProc("DeleteDC")
	procCreateDIBSection      = gdi32.NewProc("CreateDIBSection")
	procSelectObject          = gdi32.NewProc("SelectObject")
	procBitBlt                = gdi32.NewProc("BitBlt")
	procSetTextColor          = gdi32.NewProc("SetTextColor")
	procSetBkColor            = gdi32.NewProc("SetBkColor")
	procCreateSolidBrush      = gdi32.NewProc("CreateSolidBrush")
	procCreateRectRgn         = gdi32.NewProc("CreateRectRgn")
	procShellNotifyIconW      = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW         = shell32.NewProc("ShellExecuteW")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmExtendFrame        = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	procCoInitializeEx        = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance      = ole32.NewProc("CoCreateInstance")
	procSetWindowTheme        = uxtheme.NewProc("SetWindowTheme")
)

// Window messages and styles.
const (
	wmCreate         = 0x0001
	wmDestroy        = 0x0002
	wmMove           = 0x0003
	wmSize           = 0x0005
	wmActivate       = 0x0006
	wmSetFocus       = 0x0007
	wmKillFocus      = 0x0008
	wmPaint          = 0x000F
	wmClose          = 0x0010
	wmEraseBkgnd     = 0x0014
	wmSettingChange  = 0x001A
	wmSetCursor      = 0x0020
	wmMouseActivate  = 0x0021
	wmGetMinMaxInfo  = 0x0024
	wmNCCalcSize     = 0x0083
	wmNCHitTest      = 0x0084
	wmNCActivate     = 0x0086
	wmKeyDown        = 0x0100
	wmKeyUp          = 0x0101
	wmChar           = 0x0102
	wmSysKeyDown     = 0x0104
	wmSysKeyUp       = 0x0105
	wmSysChar        = 0x0106
	wmCommand        = 0x0111
	wmTimer          = 0x0113
	wmCtlColorEdit   = 0x0133
	wmCtlColorStatic = 0x0138
	wmMouseMove      = 0x0200
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmLButtonDblClk  = 0x0203
	wmRButtonDown    = 0x0204
	wmRButtonUp      = 0x0205
	wmMButtonDown    = 0x0207
	wmMButtonUp      = 0x0208
	wmMouseWheel     = 0x020A
	wmCaptureChanged = 0x0215
	wmMouseLeave     = 0x02A3
	wmDpiChanged     = 0x02E0
	wmHotkey         = 0x0312
	wmApp            = 0x8000
	wmUser           = 0x0400
	wmContextMenu    = 0x007B
	wmCopyData       = 0x004A
	wmSetFont        = 0x0030
	wmGetDlgCode     = 0x0087
	wmImeStartComp   = 0x010D
	wmImeComposition = 0x010F

	wsOverlapped       = 0x00000000
	wsPopup            = 0x80000000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsClipChildren     = 0x02000000
	wsClipSiblings     = 0x04000000
	wsCaption          = 0x00C00000
	wsSysMenu          = 0x00080000
	wsThickFrame       = 0x00040000
	wsMinimizeBox      = 0x00020000
	wsMaximizeBox      = 0x00010000
	wsTabStop          = 0x00010000
	wsOverlappedWindow = wsOverlapped | wsCaption | wsSysMenu | wsThickFrame | wsMinimizeBox | wsMaximizeBox

	wsExTopmost     = 0x00000008
	wsExToolWindow  = 0x00000080
	wsExAppWindow   = 0x00040000
	wsExNoActivate  = 0x08000000
	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020

	esAutoHScroll = 0x0080
	esMultiline   = 0x0004
	esAutoVScroll = 0x0040
	esWantReturn  = 0x1000
	esPassword    = 0x0020
	esReadOnly    = 0x0800

	emSetSel       = 0x00B1
	emGetSel       = 0x00B0
	emSetMargins   = 0x00D3
	emSetCueBanner = 0x1501
	emReplaceSel   = 0x00C2
	emSetReadOnly  = 0x00CF
	enChange       = 0x0300
	ecLeftMargin   = 1
	ecRightMargin  = 2

	swHide           = 0
	swShowNormal     = 1
	swShowNoActivate = 4
	swShow           = 5
	swRestore        = 9

	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040

	maNoActivate = 3

	htClient      = 1
	htCaption     = 2
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17

	idcArrow    = 32512
	idcIBeam    = 32513
	idcHand     = 32649
	idcCross    = 32515
	idcSizeAll  = 32646
	idcSizeNWSE = 32642
	idcSizeNESW = 32643
	idcSizeWE   = 32644
	idcSizeNS   = 32645

	csDblClks = 0x0008

	tmeLeave = 0x00000002

	monitorDefaultToNearest = 2

	mfString       = 0x0000
	mfSeparator    = 0x0800
	mfGrayed       = 0x0001
	mfChecked      = 0x0008
	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkLWin    = 0x5B
	vkRWin    = 0x5C

	dwmwaUseImmersiveDarkMode   = 20
	dwmwaWindowCornerPreference = 33
	dwmwaBorderColor            = 34
	dwmwaSystemBackdropType     = 38
	dwmwcpRound                 = 2
	dwmwcpRoundSmall            = 3
	dwmwcpDoNotRound            = 1

	errorAlreadyExists = 183
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	private uint32
}

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

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type trackMouseEventInfo struct {
	Size      uint32
	Flags     uint32
	Hwnd      uintptr
	HoverTime uint32
}

type minMaxInfo struct {
	Reserved     point
	MaxSize      point
	MaxPosition  point
	MinTrackSize point
	MaxTrackSize point
}

type margins struct{ Left, Right, Top, Bottom int32 }

func loword(v uintptr) int { return int(int16(v & 0xFFFF)) }
func hiword(v uintptr) int { return int(int16((v >> 16) & 0xFFFF)) }

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func utf16Ptr(s string) uintptr { return uintptr(unsafe.Pointer(utf16(s))) }

func moduleHandle() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

func currentThread() uint32 {
	id, _, _ := procGetCurrentThreadID.Call()
	return uint32(id)
}

// colorref packs a colour for GDI.
func colorref(r, g, b float64) uintptr {
	to := func(v float64) uintptr { return uintptr(min(max(v, 0), 1)*255 + 0.5) }
	return to(r) | to(g)<<8 | to(b)<<16
}

// keyDown reports whether a key is held, from the thread's key state.
func keyDown(vk int) bool {
	s, _, _ := procGetKeyState.Call(uintptr(vk))
	return int16(s) < 0
}

var _ = syscall.SyscallN

const srcCopy = 0x00CC0020

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}
