// Package platform wraps the operating system features modules need, with one
// implementation per system behind the same functions. Coordinates are in
// Wails' DIP space: the origin is the top-left corner of the primary screen.
package platform

import (
	"errors"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
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

// Image is a picture of the screen as top-down BGRA pixels.
type Image struct {
	Pixels        []byte
	Width, Height int
}

// Crop returns the part of img inside the rectangle at x, y of the given
// size, in its pixels and clipped to it.
func (img Image) Crop(x, y, width, height int) Image {
	x0, y0 := max(x, 0), max(y, 0)
	x1, y1 := min(x+width, img.Width), min(y+height, img.Height)
	if x1 <= x0 || y1 <= y0 {
		return Image{}
	}
	out := Image{Width: x1 - x0, Height: y1 - y0}
	out.Pixels = make([]byte, 0, out.Width*out.Height*4)
	for row := y0; row < y1; row++ {
		start := (row*img.Width + x0) * 4
		out.Pixels = append(out.Pixels, img.Pixels[start:start+out.Width*4]...)
	}
	return out
}

// ScreenText reads the text shown inside r, leaving out arubox's own windows.
// Line boxes are in pixels of the captured picture.
func ScreenText(r application.Rect) ([]TextLine, error) {
	img, err := CaptureScreen(r)
	if err != nil {
		return nil, err
	}
	return ImageText(img)
}

// minWindowSize leaves out windows too small to be worth capturing, such as
// invisible helpers.
const minWindowSize = 40
