package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void cursorPosition(int *x, int *y) {
	NSPoint p = [NSEvent mouseLocation];
	CGFloat primaryHeight = [[[NSScreen screens] firstObject] frame].size.height;
	*x = (int)p.x;
	*y = (int)(primaryHeight - p.y);
}

static void setWindowFrame(void *window, int x, int y, int width, int height) {
	NSWindow *w = (NSWindow *)window;
	CGFloat primaryHeight = [[[NSScreen screens] firstObject] frame].size.height;
	[w setFrame:NSMakeRect(x, primaryHeight - y - height, width, height) display:YES animate:NO];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// CursorPosition returns the mouse position.
func CursorPosition() application.Point {
	var x, y C.int
	application.InvokeSync(func() { C.cursorPosition(&x, &y) })
	return application.Point{X: int(x), Y: int(y)}
}

// SetWindowFrame moves and resizes a window in one step, keeping its top-left
// corner where asked. Wails' SetSize keeps the bottom-left corner on macOS and
// animates, so a window that grows with its content would jump.
func SetWindowFrame(w *application.WebviewWindow, r application.Rect) {
	application.InvokeSync(func() {
		if native := w.NativeWindow(); native != nil {
			C.setWindowFrame(native, C.int(r.X), C.int(r.Y), C.int(r.Width), C.int(r.Height))
		}
	})
}
