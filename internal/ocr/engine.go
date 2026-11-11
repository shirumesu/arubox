// Package ocr reads text off pictures of the screen for the modules that
// need it, with the system's OCR or with PP-OCRv6, a model the user
// downloads, as the user chose. Windows' own OCR knows only the languages
// whose language pack is installed; PP-OCRv6 small reads Chinese, English,
// Japanese and Latin-script languages wherever it runs.
package ocr

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/shirumesu/arubox/internal/platform"
)

// id is where the settings are kept in the settings file, like a module's.
const id = "ocr"

// The engines the user chooses between.
const (
	EngineSystem = "system"
	EnginePaddle = "ppocrv6"
)

// Settings is what the user can change about OCR.
type Settings struct {
	Engine string `json:"engine"`
	// KeepMinutes is how long PP-OCRv6 stays loaded after it was last used
	// before its memory is released. 0 keeps it for good.
	KeepMinutes int `json:"keepMinutes"`
}

// Loading takes a fraction of a second, so the model is released sooner
// than the windows are.
func defaultSettings() Settings {
	return Settings{Engine: EngineSystem, KeepMinutes: 10}
}

// Store keeps the settings. config.Store satisfies it.
type Store interface {
	ModuleSettings(id string, v any) error
	SetModuleSettings(id string, v any) error
}

// Engine reads text with the engine the user chose. It starts PP-OCRv6's
// worker on first use and ends it once it has not been used for the time
// the user chose.
type Engine struct {
	store  Store
	log    *slog.Logger
	dir    string
	assets []asset // nil where PP-OCRv6 cannot run
	client *http.Client

	mu       sync.Mutex
	settings Settings
	fetch    *fetch // the download running or last run, if any

	// run is held while PP-OCRv6 reads, starts or is released.
	run    sync.Mutex
	worker *worker // nil until used, and again once released
	uses   uint64  // counts reads, so a release timer set before the last one does nothing
}

// New creates the engine, which keeps PP-OCRv6's files in dir.
func New(store Store, log *slog.Logger, dir string) *Engine {
	settings := defaultSettings()
	if err := store.ModuleSettings(id, &settings); err != nil {
		log.Warn("ocr settings unreadable, using defaults", "error", err)
		settings = defaultSettings()
	}
	return &Engine{
		store:    store,
		log:      log,
		dir:      dir,
		assets:   assets(),
		settings: settings,
		client: &http.Client{Transport: &http.Transport{
			Proxy:                 platform.SystemProxy,
			ForceAttemptHTTP2:     true,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}},
	}
}

// ImageText reads the lines of text in a picture. Line boxes are in its
// pixels. When PP-OCRv6 is chosen and fails, the error says why; the
// system's OCR does not stand in for it.
func (e *Engine) ImageText(img platform.Image) ([]platform.TextLine, error) {
	settings := e.currentSettings()
	if settings.Engine != EnginePaddle {
		return platform.ImageText(img)
	}
	if img.Width <= 0 || img.Height <= 0 || len(img.Pixels) < img.Width*img.Height*4 {
		return nil, nil
	}
	e.run.Lock()
	defer e.run.Unlock()
	if e.worker == nil {
		if !installed(e.dir, e.assets) {
			return nil, errors.New("PP-OCRv6 的文件不完整，请在仪表盘的“文字识别”页重新下载")
		}
		start := time.Now()
		w, err := startWorker(e.dir, e.log)
		if err != nil {
			e.log.Error("start PP-OCRv6", "error", err)
			return nil, err
		}
		e.log.Info("PP-OCRv6 loaded", "took", time.Since(start))
		e.worker = w
	}
	e.uses++
	lines, err := e.worker.read(img)
	if errors.Is(err, errWorkerDied) {
		e.worker = nil
	}
	if err != nil {
		e.log.Error("read text with PP-OCRv6", "error", err)
	}
	e.scheduleRelease(settings.KeepMinutes)
	return lines, err
}

// scheduleRelease ends the worker once it has not been used for the given
// minutes. e.run must be held.
func (e *Engine) scheduleRelease(minutes int) {
	if minutes == 0 {
		return
	}
	uses := e.uses
	time.AfterFunc(time.Duration(minutes)*time.Minute, func() {
		e.run.Lock()
		defer e.run.Unlock()
		if e.uses == uses {
			e.releaseLocked()
		}
	})
}

// release ends the worker, which frees all of its memory, waiting for a
// read that is running.
func (e *Engine) release() {
	e.run.Lock()
	defer e.run.Unlock()
	e.releaseLocked()
}

func (e *Engine) releaseLocked() {
	if e.worker != nil {
		e.worker.stop()
		e.worker = nil
		e.log.Info("PP-OCRv6 released")
	}
}

func (e *Engine) loaded() bool {
	e.run.Lock()
	defer e.run.Unlock()
	return e.worker != nil
}

func (e *Engine) currentSettings() Settings {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.settings
}

func (e *Engine) setSettings(s Settings) error {
	switch {
	case s.Engine != EngineSystem && s.Engine != EnginePaddle:
		return errors.New("未知的识别引擎")
	case s.Engine == EnginePaddle && !installed(e.dir, e.assets):
		return errors.New("请先下载 PP-OCRv6")
	case s.KeepMinutes < 0:
		return errors.New("保留时间不能为负")
	}
	e.mu.Lock()
	err := e.store.SetModuleSettings(id, s)
	if err == nil {
		e.settings = s
	}
	e.mu.Unlock()
	if err != nil {
		return errors.New("保存设置失败：" + err.Error())
	}
	e.run.Lock()
	defer e.run.Unlock()
	switch {
	case s.Engine != EnginePaddle:
		e.releaseLocked()
	case e.worker != nil:
		// Count the change as a use, so the timers set for the old time
		// do nothing and the new one runs from now.
		e.uses++
		e.scheduleRelease(s.KeepMinutes)
	}
	return nil
}
