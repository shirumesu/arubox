package core

import (
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/platform"
)

// hotkeyKeyEvent carries the keys pressed while a hotkey is recorded.
const hotkeyKeyEvent = "hotkey:key"

func init() {
	application.RegisterEvent[platform.KeyPress](hotkeyKeyEvent)
}

// dashboard is opened rarely, so its window is destroyed on close and
// created again on the next open.
type dashboard struct {
	core *Core
	mu   sync.Mutex
	win  *application.WebviewWindow
	// stopKeys ends the key capture of a hotkey being recorded.
	stopKeys func()
}

// beginRecording releases the hotkeys and takes the keyboard, so that a
// combination the system or another program uses can be recorded too.
func (d *dashboard) beginRecording() {
	d.core.hotkeys.Suspend()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopKeys != nil {
		return
	}
	stop, err := platform.CaptureKeys(func(k platform.KeyPress) {
		d.core.app.Event.Emit(hotkeyKeyEvent, k)
	})
	if err != nil {
		d.core.log.Error("capture keys", "error", err)
		return
	}
	d.stopKeys = stop
}

func (d *dashboard) endRecording() {
	d.mu.Lock()
	stop := d.stopKeys
	d.stopKeys = nil
	d.mu.Unlock()
	if stop != nil {
		stop()
	}
	d.core.hotkeys.Resume()
}

func (d *dashboard) open() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.win != nil {
		d.win.Show()
		d.win.Focus()
		return
	}
	background := application.NewRGB(250, 250, 250)
	if d.core.app.Env.IsDarkMode() {
		background = application.NewRGB(23, 23, 23)
	}
	win := d.core.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "dashboard",
		Title:            appName,
		Width:            960,
		Height:           640,
		MinWidth:         760,
		MinHeight:        520,
		URL:              "/",
		BackgroundColour: background,
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 52,
		},
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		d.mu.Lock()
		d.win = nil
		d.mu.Unlock()
		// The window may close while a hotkey is being recorded.
		d.endRecording()
	})
	win.Show()
	win.Focus()
	d.win = win
}

// DashboardService is what the dashboard frontend calls. Every mutation
// returns the new state so the frontend never shows stale data.
type DashboardService struct {
	core *Core
}

// State is everything the dashboard shows.
type State struct {
	// Platform is the GOOS value: "darwin" or "windows".
	Platform  string          `json:"platform"`
	Version   string          `json:"version"`
	Autostart Autostart       `json:"autostart"`
	Modules   []module.Status `json:"modules"`
	Hotkeys   []hotkey.Status `json:"hotkeys"`
}

// Autostart reports whether the program starts at login.
type Autostart struct {
	Enabled bool   `json:"enabled"`
	Error   string `json:"error,omitempty"`
}

// State returns the current state.
func (s *DashboardService) State() State {
	c := s.core
	var auto Autostart
	if enabled, err := c.app.Autostart.IsEnabled(); err != nil {
		auto.Error = err.Error()
	} else {
		auto.Enabled = enabled
	}
	return State{
		Platform:  runtime.GOOS,
		Version:   Version,
		Autostart: auto,
		Modules:   c.modules.List(),
		Hotkeys:   c.hotkeys.List(),
	}
}

// SetAutostart turns starting at login on or off.
func (s *DashboardService) SetAutostart(enabled bool) (State, error) {
	var err error
	if enabled {
		err = s.core.app.Autostart.Enable()
	} else {
		err = s.core.app.Autostart.Disable()
	}
	if err != nil {
		s.core.log.Error("set autostart", "enabled", enabled, "error", err)
	}
	return s.State(), err
}

// SetModuleEnabled enables or disables a module.
func (s *DashboardService) SetModuleEnabled(id string, enabled bool) (State, error) {
	err := s.core.modules.SetEnabled(id, enabled)
	return s.State(), err
}

// SetHotkey binds a hotkey; an empty accelerator unbinds it.
func (s *DashboardService) SetHotkey(id, accelerator string) (State, error) {
	err := s.core.hotkeys.Set(id, accelerator)
	return s.State(), err
}

// ResetHotkey restores a hotkey's default binding.
func (s *DashboardService) ResetHotkey(id string) (State, error) {
	err := s.core.hotkeys.Reset(id)
	return s.State(), err
}

// BeginHotkeyRecording releases all hotkeys while the user presses a new
// combination, so pressing a bound one records it instead of triggering it.
// On Windows the keys then arrive as hotkey:key events rather than as key
// events in the page.
func (s *DashboardService) BeginHotkeyRecording() {
	s.core.dashboard.beginRecording()
}

// EndHotkeyRecording gives the keyboard back and registers the hotkeys again.
func (s *DashboardService) EndHotkeyRecording() {
	s.core.dashboard.endRecording()
}

// OpenLogFolder shows the log directory in Finder or Explorer.
func (s *DashboardService) OpenLogFolder() error {
	return s.core.app.Browser.OpenFile(s.core.logDir)
}
