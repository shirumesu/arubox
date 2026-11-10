package platform

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	procGetCursorPos = user32.NewProc("GetCursorPos")
)

// CursorPosition returns the mouse position.
func CursorPosition() application.Point {
	var p struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return application.PhysicalToDipPoint(application.Point{X: int(p.X), Y: int(p.Y)})
}

// SetWindowFrame moves and resizes a window in one step.
func SetWindowFrame(w *application.WebviewWindow, r application.Rect) {
	w.SetBounds(r)
}
