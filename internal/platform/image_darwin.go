package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// copyImage puts a PNG on the clipboard, as PNG and as TIFF for programs
// that only read the older type.
static bool copyImage(const void *bytes, int length) {
	@autoreleasepool {
		NSData *png = [NSData dataWithBytes:bytes length:length];
		NSImage *image = [[[NSImage alloc] initWithData:png] autorelease];
		if (image == nil) {
			return false;
		}
		NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
		[pasteboard clearContents];
		[pasteboard declareTypes:@[NSPasteboardTypePNG, NSPasteboardTypeTIFF] owner:nil];
		return [pasteboard setData:png forType:NSPasteboardTypePNG] &&
			[pasteboard setData:[image TIFFRepresentation] forType:NSPasteboardTypeTIFF];
	}
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// CopyImage puts a PNG on the clipboard.
func CopyImage(png []byte) error {
	if len(png) == 0 || !C.copyImage(unsafe.Pointer(&png[0]), C.int(len(png))) {
		return errors.New("无法写入剪贴板")
	}
	return nil
}
