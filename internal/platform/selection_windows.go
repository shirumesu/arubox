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
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
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
	slotControlViewWalker    = 14 // IUIAutomation
	slotGetParentElement     = 3  // IUIAutomationTreeWalker
	slotGetCurrentPatternAs  = 14 // IUIAutomationElement
	slotGetSelection         = 5  // IUIAutomationTextPattern
	slotRangeArrayLength     = 3  // IUIAutomationTextRangeArray
	slotRangeArrayGetElement = 4  // IUIAutomationTextRangeArray
	slotTextRangeGetText     = 12 // IUIAutomationTextRange
	clsctxInprocServer       = 1
	coinitMultithreaded      = 0
	rpcEChangedMode          = 0x80010106
)

// maxSelectionAncestors is how far up from the focused element the selection
// is looked for. In Chromium a focusable element inside a page can take the
// focus while the page's document, a few levels up, owns the selection.
const maxSelectionAncestors = 10

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
	// COM may already be set up on this thread: S_FALSE still has to be
	// balanced, and a single-threaded setup works for UI Automation too.
	switch hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded); {
	case succeeded(hr):
		defer procCoUninitialize.Call()
	case hr != rpcEChangedMode:
		return ""
	}

	var automation comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&automation)))
	if !succeeded(hr) || automation == nil {
		return ""
	}
	defer release(automation)

	var walker comObject
	if !succeeded(call(automation, slotControlViewWalker, uintptr(unsafe.Pointer(&walker)))) || walker == nil {
		return ""
	}
	defer release(walker)

	var element comObject
	if !succeeded(call(automation, slotGetFocusedElement, uintptr(unsafe.Pointer(&element)))) || element == nil {
		return ""
	}
	for range maxSelectionAncestors + 1 {
		if text := elementSelection(element); text != "" {
			release(element)
			return text
		}
		var parent comObject
		ok := succeeded(call(walker, slotGetParentElement, uintptr(element), uintptr(unsafe.Pointer(&parent))))
		release(element)
		if !ok || parent == nil {
			return ""
		}
		element = parent
	}
	release(element)
	return ""
}

// elementSelection returns the text selected in element, if it has text.
func elementSelection(element comObject) string {
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

// The copy keys are tried in turn: Ctrl+Insert first, which terminals copy
// with where Ctrl+C would interrupt, then Ctrl+C for programs that only
// know that. Each gets this long to answer; one that has nothing selected
// leaves the clipboard alone.
var copyKeys = []struct {
	vk       uint16
	extended bool
	wait     time.Duration
}{
	{vkInsert, true, 100 * time.Millisecond},
	{vkC, false, 300 * time.Millisecond},
}

const (
	// copySettle lets a program finish writing: some put several formats
	// on the clipboard one after another.
	copySettle = 10 * time.Millisecond
	// copyLate is how long a copy that has no text yet may still add it.
	copyLate = 200 * time.Millisecond
)

func copySelection() string {
	// The hotkey's keys are still held when it fires. The copy is sent once
	// they are let go, so the program sees a plain copy.
	maskModifierRelease()
	waitForKeysUp()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// The clipboard can only be written back by a window that owns it.
	owner, err := createMessageWindow()
	if err != nil {
		return ""
	}
	defer procDestroyWindow.Call(owner)

	// The clipboard is emptied, so whatever lands on it is the copy.
	saved, ok := takeClipboard(owner)
	if !ok {
		return ""
	}
	defer restoreClipboard(owner, unmonitored(saved))
	for _, k := range copyKeys {
		before, _, _ := procGetClipboardSequenceNumber.Call()
		pressCopy(k.vk, k.extended)
		if text, copied := awaitCopy(owner, uint32(before), k.wait); copied {
			return text
		}
	}
	return ""
}

// awaitCopy waits for the clipboard to change from sequence number before
// and returns its text. copied is false when it did not change in time.
func awaitCopy(owner uintptr, before uint32, wait time.Duration) (text string, copied bool) {
	sequence := func() uint32 {
		n, _, _ := procGetClipboardSequenceNumber.Call()
		return uint32(n)
	}
	deadline := time.Now().Add(wait)
	for sequence() == before {
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(5 * time.Millisecond)
	}
	deadline = time.Now().Add(copyLate)
	for {
		time.Sleep(copySettle)
		seen := sequence()
		if text := clipboardText(owner); text != "" {
			return text, true
		}
		for sequence() == seen {
			if time.Now().After(deadline) {
				return "", true
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}
