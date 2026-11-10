package screenshot

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/platform"
)

// Service is what the editor, the pins and the dashboard's settings page
// call. Editor calls name the capture they are about, so a late call about
// an older one does nothing.
type Service struct {
	module *Module
}

// SettingsState is everything the settings page shows.
type SettingsState struct {
	Settings Settings `json:"settings"`
	// ScreenCapture reports whether macOS lets arubox read the screen. It is
	// always true on Windows, which has no such permission.
	ScreenCapture bool `json:"screenCapture"`
	// Platform is the GOOS value: "darwin" or "windows".
	Platform string `json:"platform"`
}

// State returns the settings and the permission state.
func (s *Service) State() SettingsState {
	return SettingsState{
		Settings:      s.module.currentSettings(),
		ScreenCapture: platform.ScreenCaptureAllowed(),
		Platform:      runtime.GOOS,
	}
}

// SetSettings saves new settings.
func (s *Service) SetSettings(settings Settings) (SettingsState, error) {
	err := s.module.setSettings(settings)
	return s.State(), err
}

// OpenScreenCaptureSettings opens the system page where the screen
// recording permission is granted.
func (s *Service) OpenScreenCaptureSettings() {
	platform.OpenScreenCaptureSettings()
}

// Attach is called by the editor page once it has loaded. It returns the
// capture that arrived while the page was loading, or nil.
func (s *Service) Attach() *Start {
	if o := s.module.currentOverlay(); o != nil {
		return o.attach()
	}
	return nil
}

// Present shows the editor once the page has drawn capture seq.
func (s *Service) Present(seq int) {
	if o := s.module.currentOverlay(); o != nil {
		o.present(seq)
	}
}

// Cancel closes the editor.
func (s *Service) Cancel(seq int) {
	s.module.finish(seq)
}

// Copy closes the editor and puts the finished picture, a PNG, on the
// clipboard.
func (s *Service) Copy(seq int, png []byte) error {
	return s.module.copyImage(seq, png)
}

// Save closes the editor and asks where to save the finished picture.
func (s *Service) Save(seq int, png []byte) error {
	return s.module.save(seq, png)
}

// Pin closes the editor and pins the finished picture where it was
// selected, given in points relative to the screen.
func (s *Service) Pin(seq int, png []byte, x, y, width, height int) {
	s.module.pin(seq, png, application.Rect{X: x, Y: y, Width: width, Height: height})
}

// ReadText returns the text in the selection, given in the capture's
// pixels, line by line. The editor stays open to show it.
func (s *Service) ReadText(seq, x, y, width, height int) (string, error) {
	return s.module.readText(seq, application.Rect{X: x, Y: y, Width: width, Height: height})
}

// CopyText closes the editor and copies text read from the selection.
func (s *Service) CopyText(seq int, text string) error {
	return s.module.copyText(seq, text)
}

// Translate closes the editor and translates the text in the selection,
// given in the capture's pixels, in the translation popup.
func (s *Service) Translate(seq, x, y, width, height int) error {
	return s.module.translate(seq, application.Rect{X: x, Y: y, Width: width, Height: height})
}

// ZoomPin scales a pinned picture by factor.
func (s *Service) ZoomPin(id int, factor float64) {
	s.module.pins.zoom(id, factor)
}

// ClosePin closes a pinned picture.
func (s *Service) ClosePin(id int) {
	s.module.pins.close(id)
}

// CopyPin puts a pinned picture on the clipboard.
func (s *Service) CopyPin(id int) error {
	png, ok := s.module.pins.picture(id)
	if !ok {
		return nil
	}
	return platform.CopyImage(png)
}
