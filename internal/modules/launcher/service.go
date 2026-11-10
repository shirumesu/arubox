package launcher

import (
	"context"
	"runtime"
)

// Service is what the panel and the dashboard's settings page call.
type Service struct {
	module *Module
}

// SettingsState is everything the settings page shows.
type SettingsState struct {
	Settings Settings `json:"settings"`
	// Platform is the GOOS value: "darwin" or "windows".
	Platform string `json:"platform"`
}

// State returns the settings.
func (s *Service) State() SettingsState {
	return SettingsState{Settings: s.module.currentSettings(), Platform: runtime.GOOS}
}

// SetSettings saves new settings.
func (s *Service) SetSettings(settings Settings) (SettingsState, error) {
	err := s.module.setSettings(settings)
	return s.State(), err
}

// Search returns the results that come at once: a command, a calculation and
// the matching apps.
func (s *Service) Search(query string) []Result {
	return s.module.search(query)
}

// SearchFiles returns the files and folders whose name matches. It takes
// longer than Search, and a newer query cancels it through ctx.
func (s *Service) SearchFiles(ctx context.Context, query string) []Result {
	return s.module.searchFiles(ctx, query)
}

// Activate opens a result. reveal shows a file or folder in Finder or File
// Explorer instead of opening it.
func (s *Service) Activate(result Result, reveal bool) error {
	return s.module.activate(result, reveal)
}

// Attach is called by the panel page once it has loaded. It reports whether
// the panel was opened while the page was loading.
func (s *Service) Attach() bool {
	if p := s.module.currentPanel(); p != nil {
		return p.attach()
	}
	return false
}

// Present shows the panel at the page's height, after the page has drawn.
// seq numbers the page's height reports, which can arrive out of order.
func (s *Service) Present(seq, height int) {
	if p := s.module.currentPanel(); p != nil {
		p.present(seq, height)
	}
}

// Resize follows the page's height. seq numbers the page's height reports.
func (s *Service) Resize(seq, height int) {
	if p := s.module.currentPanel(); p != nil {
		p.resize(seq, height)
	}
}

// Hide hides the panel.
func (s *Service) Hide() {
	if p := s.module.currentPanel(); p != nil {
		p.close()
	}
}
