// Package platform wraps the Windows features modules need. Screen
// coordinates are physical pixels of the virtual screen: the origin is the
// top-left corner of the primary display.
package platform

// Rect is a rectangle in physical screen pixels.
type Rect struct {
	X, Y, Width, Height int
}

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
	// ID is what OpenApp and Icon take: an AppsFolder ID.
	ID   string
	Name string
	// Alias is another name the app goes by, such as its English name next
	// to a localized one. It may be empty.
	Alias string
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
