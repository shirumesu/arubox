package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>

static bool accessibilityTrusted(bool prompt) {
	NSDictionary *options = @{(id)kAXTrustedCheckOptionPrompt: @(prompt)};
	return AXIsProcessTrustedWithOptions((CFDictionaryRef)options);
}

static void openAccessibilitySettings(void) {
	NSURL *url = [NSURL URLWithString:@"x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"];
	[[NSWorkspace sharedWorkspace] openURL:url];
}
*/
import "C"

// AccessibilityTrusted reports whether the user granted the accessibility
// permission.
func AccessibilityTrusted() bool {
	return bool(C.accessibilityTrusted(false))
}

// RequestAccessibility shows the system prompt that leads the user to grant
// the accessibility permission, unless it is already granted.
func RequestAccessibility() {
	C.accessibilityTrusted(true)
}

// OpenAccessibilitySettings opens the accessibility page of System Settings.
func OpenAccessibilitySettings() {
	C.openAccessibilitySettings()
}
