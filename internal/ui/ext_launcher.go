package ui

import "unsafe"

// TextSelection returns the selection in text box id, in UTF-16 units; an
// empty selection is the caret. ok is false when the frame has no such box.
// The launcher uses it to let Ctrl+C copy a result only while no text is
// selected.
func (f *Frame) TextSelection(id string) (start, end int, ok bool) {
	e := f.w.edits[id]
	if e == nil || e.hwnd == 0 {
		return 0, 0, false
	}
	var s, en uint32
	procSendMessageW.Call(e.hwnd, emGetSel, uintptr(unsafe.Pointer(&s)), uintptr(unsafe.Pointer(&en)))
	return int(s), int(en), true
}
