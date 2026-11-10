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
	// Currencies are the codes a home currency can be chosen from.
	Currencies []string `json:"currencies"`
	// DefaultKeywords are the website keywords the launcher comes with.
	DefaultKeywords []Keyword `json:"defaultKeywords"`
	// Platform is the GOOS value: "darwin" or "windows".
	Platform string `json:"platform"`
}

// State returns the settings.
func (s *Service) State() SettingsState {
	return SettingsState{
		Settings:        s.module.currentSettings(),
		Currencies:      currencies,
		DefaultKeywords: defaultKeywords,
		Platform:        runtime.GOOS,
	}
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

// SearchMore returns the results that take longer than Search's: a currency
// conversion and the matching files. A newer query cancels it through ctx.
func (s *Service) SearchMore(ctx context.Context, query string) []Result {
	return s.module.searchMore(ctx, query)
}

// Translate returns a row with the query's translation, or none when the
// query is not text to translate or translation is off. always translates
// any text, for the translate keyword. A newer query cancels it through ctx.
func (s *Service) Translate(ctx context.Context, query string, always bool) []Result {
	return s.module.translate(ctx, query, always)
}

// Files returns the files and folders whose name matches the query, for the
// files keyword. A newer query cancels it through ctx.
func (s *Service) Files(ctx context.Context, query string) []Result {
	return s.module.searchFiles(ctx, query, maxFiles)
}

// FileIndex reports how far the file index is.
func (s *Service) FileIndex() FileIndexStatus {
	return s.module.fileIndexStatus()
}

// InstallFileIndex installs the Windows index service, asking the user for
// administrator rights, or installs it again over an outdated one.
func (s *Service) InstallFileIndex() (FileIndexStatus, error) {
	err := s.module.changeFileIndex(true)
	return s.module.fileIndexStatus(), err
}

// RemoveFileIndex removes the Windows index service and the index.
func (s *Service) RemoveFileIndex() (FileIndexStatus, error) {
	err := s.module.changeFileIndex(false)
	return s.module.fileIndexStatus(), err
}

// Activate opens a result. reveal shows a file or folder in Finder or File
// Explorer, or a translation in the translation popup, instead.
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
