package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics
#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>
#include <unistd.h>

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

// onScreenWindows lists the bounds of the normal windows on screen, front
// to back, leaving out arubox's own, as "x y width height\n" lines in
// global points with the origin at the top-left of the primary screen.
static char *onScreenWindows(void) {
	@autoreleasepool {
		NSMutableString *out = [NSMutableString string];
		CFArrayRef list = CGWindowListCopyWindowInfo(
			kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
		if (list == NULL) {
			return strdup("");
		}
		for (NSDictionary *info in (NSArray *)list) {
			if ([info[(id)kCGWindowLayer] intValue] != 0 || [info[(id)kCGWindowOwnerPID] intValue] == getpid() ||
				[info[(id)kCGWindowAlpha] doubleValue] <= 0) {
				continue;
			}
			CGRect r;
			if (!CGRectMakeWithDictionaryRepresentation((CFDictionaryRef)info[(id)kCGWindowBounds], &r)) {
				continue;
			}
			[out appendFormat:@"%d %d %d %d\n", (int)r.origin.x, (int)r.origin.y, (int)r.size.width, (int)r.size.height];
		}
		CFRelease(list);
		return strdup([out UTF8String]);
	}
}
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

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

// OnScreenWindows returns the bounds of the normal windows on screen, front
// to back, leaving out arubox's own.
func OnScreenWindows() []application.Rect {
	text := C.onScreenWindows()
	defer C.free(unsafe.Pointer(text))
	var rects []application.Rect
	for line := range strings.Lines(C.GoString(text)) {
		var r application.Rect
		if _, err := fmt.Sscanf(line, "%d %d %d %d", &r.X, &r.Y, &r.Width, &r.Height); err == nil && r.Width >= minWindowSize && r.Height >= minWindowSize {
			rects = append(rects, r)
		}
	}
	return rects
}
