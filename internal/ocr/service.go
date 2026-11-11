package ocr

import (
	"errors"
	"runtime"
)

var errUnsupported = errors.New("PP-OCRv6 没有适用于这台电脑的版本")

// Service is what the dashboard's OCR page calls. Every call returns the
// new state.
type Service struct {
	engine *Engine
}

// NewService binds the engine for the dashboard.
func NewService(e *Engine) *Service { return &Service{engine: e} }

// State is everything the page shows.
type State struct {
	// Platform is the GOOS value: "darwin" or "windows".
	Platform string   `json:"platform"`
	Settings Settings `json:"settings"`
	// Supported reports whether PP-OCRv6 can run on this computer.
	Supported bool `json:"supported"`
	Installed bool `json:"installed"`
	// Loaded reports whether PP-OCRv6 is in memory now.
	Loaded bool   `json:"loaded"`
	Files  []File `json:"files"`
	// Download is the download running or last run since arubox started,
	// or nil.
	Download *Progress `json:"download"`
}

// File is a file PP-OCRv6 needs, as the page lists it before download.
type File struct {
	Label   string `json:"label"`
	License string `json:"license"`
	// DownloadSize and Size are in bytes: what is fetched, and what is
	// kept once placed.
	DownloadSize int64    `json:"downloadSize"`
	Size         int64    `json:"size"`
	Sources      []Source `json:"sources"`
}

// State returns the current state.
func (s *Service) State() State {
	e := s.engine
	st := State{
		Platform:  runtime.GOOS,
		Settings:  e.currentSettings(),
		Supported: e.assets != nil,
		Installed: installed(e.dir, e.assets),
		Loaded:    e.loaded(),
		Files:     []File{},
	}
	for _, a := range e.assets {
		st.Files = append(st.Files, File{
			Label: a.label, License: a.license,
			DownloadSize: a.download.size, Size: a.size, Sources: a.download.sources,
		})
	}
	e.mu.Lock()
	if e.fetch != nil {
		p := e.fetch.progress()
		st.Download = &p
	}
	e.mu.Unlock()
	return st
}

// SetSettings saves new settings. PP-OCRv6 can be chosen once it is
// downloaded.
func (s *Service) SetSettings(settings Settings) (State, error) {
	err := s.engine.setSettings(settings)
	return s.State(), err
}

// Download starts fetching PP-OCRv6's files in the background; State tells
// how it goes.
func (s *Service) Download() (State, error) {
	err := s.engine.startDownload()
	return s.State(), err
}

// CancelDownload stops the download. Files already fetched are kept.
func (s *Service) CancelDownload() State {
	s.engine.cancelDownload()
	return s.State()
}

// Delete removes PP-OCRv6's files and goes back to the system's OCR if it
// was chosen.
func (s *Service) Delete() (State, error) {
	e := s.engine
	if e.downloading() {
		return s.State(), errors.New("正在下载，请先取消")
	}
	if settings := e.currentSettings(); settings.Engine == EnginePaddle {
		settings.Engine = EngineSystem
		if err := e.setSettings(settings); err != nil {
			return s.State(), err
		}
	}
	// Unloading the library first lets Windows delete it.
	e.release()
	err := e.remove()
	return s.State(), err
}
