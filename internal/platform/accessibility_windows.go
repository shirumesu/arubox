package platform

// Windows lets any program read other programs' selection, so there is no
// permission to ask for.

// AccessibilityTrusted reports whether the user granted the accessibility
// permission. It always has on Windows.
func AccessibilityTrusted() bool { return true }

// RequestAccessibility does nothing on Windows.
func RequestAccessibility() {}

// OpenAccessibilitySettings does nothing on Windows.
func OpenAccessibilitySettings() {}
