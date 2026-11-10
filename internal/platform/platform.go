// Package platform wraps the operating system features modules need, with one
// implementation per system behind the same functions. Coordinates are in
// Wails' DIP space: the origin is the top-left corner of the primary screen.
package platform

import (
	"errors"
	"net/http"
	"strconv"
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

// Strips is how many horizontal strips a frozen screen goes to a page in.
// The page fetches them all at once, and WebView2 hands several responses
// over side by side: eight strips arrive in about half the time of one
// response of the whole picture.
const Strips = 8

// Picture tells a page the size of a frozen screen it loads in strips.
type Picture struct {
	// Width and Height are in pixels.
	Width  int `json:"width"`
	Height int `json:"height"`
	Strips int `json:"strips"`
}

// Picture describes img for loading it in strips.
func (img Image) Picture() Picture {
	return Picture{Width: img.Width, Height: img.Height, Strips: Strips}
}

// ServeStrip writes strip part of img's Strips as RGBA rows, which the page
// puts on a canvas as they are, without decoding an image file. A strip
// holds the picture's height divided by Strips, rounded up, so the last one
// may be shorter.
func (img Image) ServeStrip(w http.ResponseWriter, r *http.Request, part int) {
	rows := (img.Height + Strips - 1) / Strips
	y0, y1 := part*rows, min((part+1)*rows, img.Height)
	if part < 0 || y0 >= y1 {
		http.NotFound(w, r)
		return
	}
	src := img.Pixels[y0*img.Width*4 : y1*img.Width*4]
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += 4 {
		out[i], out[i+1], out[i+2], out[i+3] = src[i+2], src[i+1], src[i], 255
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	_, _ = w.Write(out)
}

// KeyPress is a key going down or up while CaptureKeys holds the keyboard.
type KeyPress struct {
	// Key is the key's name in the accelerator syntax, or "" for modifier
	// keys and keys a hotkey cannot use.
	Key string `json:"key"`
	// Modifiers are the modifiers held after this event, in the
	// accelerator order: Ctrl, Alt, Shift, Super.
	Modifiers []string `json:"modifiers"`
	Down      bool     `json:"down"`
	// Idle reports that no key is held any more.
	Idle bool `json:"idle"`
}

// minWindowSize leaves out windows too small to be worth capturing, such as
// invisible helpers.
const minWindowSize = 40
