// Package module defines the contract between the core and its tool modules,
// and the registry that enables and disables them.
package module

import (
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/hotkey"
)

// Info describes a module.
type Info struct {
	// ID is a stable identifier used in settings and hotkey ids.
	ID          string
	Name        string
	Description string
	// Platforms lists the GOOS values the module runs on.
	Platforms []string
}

// Context is what the core hands a module when enabling it.
type Context struct {
	App *application.App
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
}
