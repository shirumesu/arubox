// Package platform wraps the operating system features modules need, with one
// implementation per system behind the same functions. Coordinates are in
// Wails' DIP space: the origin is the top-left corner of the primary screen.
package platform

import (
	"errors"
	"time"
)

// ErrNoAccessibility is returned when macOS has not granted the accessibility
// permission that reading another program's selection requires.
var ErrNoAccessibility = errors.New("需要辅助功能权限")

// ErrNoScreenCapture is returned when macOS has not granted the screen
// recording permission that reading text off the screen requires.
var ErrNoScreenCapture = errors.New("需要屏幕录制权限")

// TextLine is one line of text read off the screen.
type TextLine struct {
	Text string
	Box  Box
}

// Box is a rectangle with the origin at the top-left. Its unit depends on
// where it comes from.
type Box struct {
	X, Y, Width, Height float64
}

// App is an installed application.
type App struct {
	// ID is what OpenApp and Icon take: a bundle path on macOS, an
	// AppsFolder ID on Windows.
	ID   string
	Name string
	// Alias is another name the app goes by, such as its English name next
	// to a localized one. It may be empty.
	Alias string
}

// FoundFile is a file or folder found by name.
type FoundFile struct {
	Path string
	// LastUsed is when the user last opened it, or zero if unknown.
	LastUsed time.Time
}
