package ui

var procIsIconic = user32.NewProc("IsIconic")

// Restore brings a minimised window back to its place, as Show alone
// leaves it minimised. Call on the UI thread.
func (w *Window) Restore() {
	if w.destroyed {
		return
	}
	if r, _, _ := procIsIconic.Call(w.hwnd); r != 0 {
		procShowWindow.Call(w.hwnd, swRestore)
	}
}
