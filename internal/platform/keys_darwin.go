package platform

// CaptureKeys does nothing on macOS: the page reads the keys itself, so
// shortcuts the system takes first, such as ⌘Space, cannot be recorded.
func CaptureKeys(func(KeyPress)) (stop func(), err error) {
	return func() {}, nil
}
