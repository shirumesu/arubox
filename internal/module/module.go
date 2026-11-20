// Package module defines the contract between the core and its tool modules,
// and the registry that enables and disables them.
package module

import (
	"log/slog"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/ui"
)

// Info describes a module.
type Info struct {
	// ID is a stable identifier used in settings and hotkey ids.
	ID          string
	Name        string
	Description string
	// Icon is a ui.Icon glyph for the dashboard.
	Icon rune
}

// Context is what the core hands a module when enabling it.
type Context struct {
	Log *slog.Logger
}

// Module is one tool. A module does no work and holds no resources until
// Enable, and releases everything in Disable.
type Module interface {
	Info() Info
	// Hotkeys declares the module's hotkeys. They are registered after
	// Enable succeeds and unregistered before Disable.
	Hotkeys() []hotkey.Def
	Enable(ctx Context) error
	Disable() error
	// Settings returns the module's page in the dashboard, or nil. The page
	// works whether the module is enabled or not.
	Settings() Page
}

// Settler is a module whose Disable leaves work running in the background,
// such as saving a large file, so that turning it off does not hold up the
// dashboard. The registry waits for that work before the program exits.
type Settler interface {
	// Settle waits for the work Disable left running. It runs on the UI
	// thread, like Disable.
	Settle()
}

// Page is one page of the dashboard.
type Page interface {
	// Draw draws the page's content in the column from x to x+w, starting
	// at y, and returns the y below it. It runs on the UI thread for every
	// frame of the dashboard, so it must not block: slow work runs on a
	// goroutine that calls ui.Post and Invalidate when done.
	Draw(f *ui.Frame, x, y, w float64) float64
}

// Opener is a page that wants to know when the dashboard shows it, to load
// fresh state.
type Opener interface {
	Opened(w *ui.Window)
}
