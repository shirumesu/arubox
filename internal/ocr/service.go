package ocr

import "errors"

var errUnsupported = errors.New("PP-OCRv6 没有适用于这台电脑的版本")

// Status is what the dashboard's OCR page shows. Getting it may wait for a
// read that is running, so the page asks for it off the UI thread.
type Status struct {
	Settings Settings
	// Supported reports whether PP-OCRv6 can run on this computer.
	Supported bool
	Installed bool
	// Loaded reports whether PP-OCRv6 is in memory now.
	Loaded bool
	Files  []File
	// Download is the download running or last run since arubox started,
	// or nil.
	Download *Progress
}

// File is a file PP-OCRv6 needs, as the page lists it before download.
type File struct {
	Label   string
	License string
	// DownloadSize and Size are in bytes: what is fetched, and what is
	// kept once placed.
	DownloadSize int64
	Size         int64
	Sources      []Source
}

// Status returns the current status.
func (e *Engine) Status() Status {
	st := Status{
		Settings:  e.currentSettings(),
		Supported: e.assets != nil,
		Installed: installed(e.dir, e.assets),
		Loaded:    e.loaded(),
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
func (e *Engine) SetSettings(s Settings) error { return e.setSettings(s) }

// Download starts fetching PP-OCRv6's files in the background; Status
// tells how it goes.
func (e *Engine) Download() error { return e.startDownload() }

// CancelDownload stops the download. Files already fetched are kept.
func (e *Engine) CancelDownload() { e.cancelDownload() }

// Delete removes PP-OCRv6's files and goes back to the system's OCR if it
// was chosen. It waits for a read that is running.
func (e *Engine) Delete() error {
	if e.downloading() {
		return errors.New("正在下载，请先取消")
	}
	if settings := e.currentSettings(); settings.Engine == EnginePaddle {
		settings.Engine = EngineSystem
		if err := e.setSettings(settings); err != nil {
			return err
		}
	}
	// Unloading the library first lets Windows delete it.
	e.release()
	return e.remove()
}
