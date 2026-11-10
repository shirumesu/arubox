package platform

import (
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	oleaut32             = windows.NewLazySystemDLL("oleaut32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procSysStringLen     = oleaut32.NewProc("SysStringLen")
	procSysFreeString    = oleaut32.NewProc("SysFreeString")
)

var (
	clsidCUIAutomation       = windows.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidIUIAutomation         = windows.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	iidIUIAutomationTextPatt = windows.GUID{Data1: 0x32eba289, Data2: 0x3583, Data3: 0x42c9, Data4: [8]byte{0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a}}
)

const uiaTextPatternID = 10014

// Method slots in the COM interfaces' virtual tables (UIAutomationClient.h).
const (
	slotGetFocusedElement    = 8  // IUIAutomation
	slotGetCurrentPatternAs  = 14 // IUIAutomationElement
	slotGetSelection         = 5  // IUIAutomationTextPattern
	slotRangeArrayLength     = 3  // IUIAutomationTextRangeArray
	slotRangeArrayGetElement = 4  // IUIAutomationTextRangeArray
	slotTextRangeGetText     = 12 // IUIAutomationTextRange
	clsctxInprocServer       = 1
	coinitMultithreaded      = 0
)

// SelectedText returns the text selected in the frontmost program, or "" when
// nothing is selected. It asks UI Automation first and falls back to a
// simulated copy, putting the clipboard back afterwards.
func SelectedText() (string, error) {
	if text := uiaSelectedText(); text != "" {
		return text, nil
	}
	return copySelection(), nil
}

func uiaSelectedText() string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, coinitMultithreaded); err != nil {
		return ""
	}
	defer windows.CoUninitialize()

	var automation comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&automation)))
	if !succeeded(hr) || automation == nil {
		return ""
	}
	defer release(automation)

	var element comObject
	if !succeeded(call(automation, slotGetFocusedElement, uintptr(unsafe.Pointer(&element)))) || element == nil {
		return ""
	}
	defer release(element)

	var pattern comObject
	if !succeeded(call(element, slotGetCurrentPatternAs, uiaTextPatternID,
		uintptr(unsafe.Pointer(&iidIUIAutomationTextPatt)), uintptr(unsafe.Pointer(&pattern)))) || pattern == nil {
		return ""
	}
	defer release(pattern)

	var ranges comObject
	if !succeeded(call(pattern, slotGetSelection, uintptr(unsafe.Pointer(&ranges)))) || ranges == nil {
		return ""
	}
	defer release(ranges)

	var count int32
	if !succeeded(call(ranges, slotRangeArrayLength, uintptr(unsafe.Pointer(&count)))) {
		return ""
	}
	var text []uint16
	for i := range count {
		var r comObject
		if !succeeded(call(ranges, slotRangeArrayGetElement, uintptr(i), uintptr(unsafe.Pointer(&r)))) || r == nil {
			continue
		}
		var bstr *uint16
		if succeeded(call(r, slotTextRangeGetText, ^uintptr(0), uintptr(unsafe.Pointer(&bstr)))) && bstr != nil {
			n, _, _ := procSysStringLen.Call(uintptr(unsafe.Pointer(bstr)))
			text = append(text, unsafe.Slice(bstr, n)...)
			procSysFreeString.Call(uintptr(unsafe.Pointer(bstr)))
		}
		release(r)
	}
	return windows.UTF16ToString(text)
}

func copySelection() string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// The clipboard can only be written back by a window that owns it.
	owner, err := createMessageWindow()
	if err != nil {
		return ""
	}
	defer procDestroyWindow.Call(owner)

	saved, ok := saveClipboard(owner)
	if !ok {
		return ""
	}
	before, _, _ := procGetClipboardSequenceNumber.Call()
	pressCopy()
	// Programs answer a copy within a few milliseconds; one that has nothing
	// selected leaves the clipboard alone.
	deadline := time.Now().Add(300 * time.Millisecond)
	changed := func() bool {
		n, _, _ := procGetClipboardSequenceNumber.Call()
		return n != before
	}
	for !changed() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !changed() {
		return ""
	}
	text := clipboardText(owner)
	restoreClipboard(owner, saved)
	return text
}
