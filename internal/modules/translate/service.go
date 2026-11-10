package translate

import (
	"context"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/platform"
)

// Service is what the popup and the dashboard's settings page call.
type Service struct {
	module *Module
}

// SettingsState is everything the settings page shows.
type SettingsState struct {
	Settings Settings `json:"settings"`
	// Accessibility reports whether macOS lets arubox read selections, and
	// ScreenCapture whether it lets arubox read the screen. Both are always
	// true on Windows, which has no such permissions.
	Accessibility bool `json:"accessibility"`
	ScreenCapture bool `json:"screenCapture"`
	// Platform is the GOOS value: "darwin" or "windows".
	Platform string `json:"platform"`
}

// State returns the settings and the permission state.
func (s *Service) State() SettingsState {
	return SettingsState{
		Settings:      s.module.currentSettings(),
		Accessibility: platform.AccessibilityTrusted(),
		ScreenCapture: platform.ScreenCaptureAllowed(),
		Platform:      runtime.GOOS,
	}
}

// SetSettings saves new settings.
func (s *Service) SetSettings(settings Settings) (SettingsState, error) {
	err := s.module.setSettings(settings)
	return s.State(), err
}

// OpenPermissionSettings opens the system page where a permission is
// granted: "accessibility" or "screen".
func (s *Service) OpenPermissionSettings(permission string) {
	switch permission {
	case permissionAccessibility:
		platform.OpenAccessibilitySettings()
	case permissionScreen:
		platform.OpenScreenCaptureSettings()
	}
}

// Translate translates text with the current settings. Closing the popup
// or sending a newer text cancels it through ctx.
func (s *Service) Translate(ctx context.Context, text string) (Translation, error) {
	return s.module.translate(ctx, strings.TrimSpace(text))
}

// Attach is called by the popup page once it listens for requests. It
// returns the request that arrived while the page was loading, or nil.
func (s *Service) Attach() *Request {
	if p := s.module.currentPopup(); p != nil {
		return p.attach()
	}
	return nil
}

// Present shows the popup at the page's height, after the page has drawn the
// request it was opened with. seq numbers the page's height reports, which
// can arrive out of order.
func (s *Service) Present(seq, height int) {
	if p := s.module.currentPopup(); p != nil {
		p.present(seq, height)
	}
}

// Resize follows the page's height. seq numbers the page's height reports.
func (s *Service) Resize(seq, height int) {
	if p := s.module.currentPopup(); p != nil {
		p.resize(seq, height)
	}
}

// SetPinned keeps the popup open when it loses focus.
func (s *Service) SetPinned(pinned bool) {
	if p := s.module.currentPopup(); p != nil {
		p.setPinned(pinned)
	}
}

// Hide hides the popup.
func (s *Service) Hide() {
	if p := s.module.currentPopup(); p != nil {
		p.close()
	}
}

// AttachCapture is called by the capture page once it has loaded.
func (s *Service) AttachCapture() {
	if c := s.module.currentCapture(); c != nil {
		c.attach()
	}
}

// FinishCapture translates the text in the area the user dragged out, given
// in the capture window's coordinates.
func (s *Service) FinishCapture(x, y, width, height int) {
	s.module.readScreen(application.Rect{X: x, Y: y, Width: width, Height: height})
}

// CancelCapture hides the capture window.
func (s *Service) CancelCapture() {
	if c := s.module.currentCapture(); c != nil {
		c.cancel()
	}
}
