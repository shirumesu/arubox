package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>

// axSelectedText asks the focused element for its selection through the
// accessibility API. It returns NULL when the element does not say.
static char *axSelectedText(void) {
	char *result = NULL;
	AXUIElementRef system = AXUIElementCreateSystemWide();
	// A hung program must not hang the hotkey.
	AXUIElementSetMessagingTimeout(system, 0.5);
	AXUIElementRef focused = NULL;
	if (AXUIElementCopyAttributeValue(system, kAXFocusedUIElementAttribute, (CFTypeRef *)&focused) == kAXErrorSuccess && focused != NULL) {
		CFTypeRef text = NULL;
		if (AXUIElementCopyAttributeValue(focused, kAXSelectedTextAttribute, &text) == kAXErrorSuccess && text != NULL) {
			if (CFGetTypeID(text) == CFStringGetTypeID() && CFStringGetLength(text) > 0) {
				result = strdup([(NSString *)text UTF8String]);
			}
			CFRelease(text);
		}
		CFRelease(focused);
	}
	CFRelease(system);
	return result;
}

static long pasteboardChangeCount(void) {
	return [[NSPasteboard generalPasteboard] changeCount];
}

// savePasteboard copies every item on the clipboard, in every type, so it
// can be put back after the simulated copy.
static void *savePasteboard(void) {
	NSMutableArray *saved = [[NSMutableArray alloc] init];
	for (NSPasteboardItem *item in [[NSPasteboard generalPasteboard] pasteboardItems]) {
		NSPasteboardItem *copy = [[NSPasteboardItem alloc] init];
		for (NSPasteboardType type in [item types]) {
			NSData *data = [item dataForType:type];
			if (data != nil) {
				[copy setData:data forType:type];
			}
		}
		[saved addObject:copy];
		[copy release];
	}
	return saved;
}

static void restorePasteboard(void *saved) {
	NSArray *items = (NSArray *)saved;
	NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
	[pasteboard clearContents];
	if ([items count] > 0) {
		[pasteboard writeObjects:items];
	}
	[items release];
}

static void releasePasteboard(void *saved) {
	[(NSArray *)saved release];
}

static char *pasteboardText(void) {
	NSString *text = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
	return text != nil ? strdup([text UTF8String]) : NULL;
}

// postCopy presses Command-C. The flags are set explicitly so modifiers the
// user still holds from the hotkey do not turn it into another shortcut.
static void postCopy(void) {
	CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStatePrivate);
	CGKeyCode c = 8; // kVK_ANSI_C
	CGEventRef down = CGEventCreateKeyboardEvent(source, c, true);
	CGEventRef up = CGEventCreateKeyboardEvent(source, c, false);
	CGEventSetFlags(down, kCGEventFlagMaskCommand);
	CGEventSetFlags(up, kCGEventFlagMaskCommand);
	CGEventPost(kCGSessionEventTap, down);
	CGEventPost(kCGSessionEventTap, up);
	CFRelease(down);
	CFRelease(up);
	if (source != NULL) {
		CFRelease(source);
	}
}
*/
import "C"

import (
	"time"
	"unsafe"
)

// SelectedText returns the text selected in the frontmost program, or "" when
// nothing is selected. It asks the accessibility API first and falls back to
// a simulated copy, putting the clipboard back afterwards.
func SelectedText() (string, error) {
	if !AccessibilityTrusted() {
		return "", ErrNoAccessibility
	}
	if text := takeCString(C.axSelectedText()); text != "" {
		return text, nil
	}
	return copySelection(), nil
}

func copySelection() string {
	before := C.pasteboardChangeCount()
	saved := C.savePasteboard()
	C.postCopy()
	// Programs answer a copy within a few milliseconds; one that has nothing
	// selected leaves the clipboard alone.
	deadline := time.Now().Add(300 * time.Millisecond)
	for C.pasteboardChangeCount() == before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if C.pasteboardChangeCount() == before {
		C.releasePasteboard(saved)
		return ""
	}
	text := takeCString(C.pasteboardText())
	C.restorePasteboard(saved)
	return text
}

func takeCString(s *C.char) string {
	if s == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(s))
	return C.GoString(s)
}
