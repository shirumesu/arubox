// Package update keeps the program current. It looks for a newer release
// on GitHub when the program starts and every few hours, and on request;
// once the user agrees it downloads the package, checks it against the
// signed manifest, puts it in place of the running copy and restarts.
package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/platform"
)

// stateEvent carries the updater's state to the dashboard on every change.
const stateEvent = "update:state"

func init() {
	application.RegisterEvent[State](stateEvent)
}

// checkEvery is how often a running program looks for a new release.
const checkEvery = 6 * time.Hour

// archName is the processor architecture as release files name it.
func archName() string {
	if runtime.GOARCH == "amd64" {
		return "x64"
	}
	return runtime.GOARCH
}

// Phase is where the updater is.
type Phase string

const (
	// PhaseIdle means no check has finished yet.
	PhaseIdle Phase = "idle"
	// PhaseChecking means a check is running.
	PhaseChecking Phase = "checking"
	// PhaseLatest means the last check found nothing newer.
	PhaseLatest Phase = "latest"
	// PhaseAvailable means a newer release is on offer.
	PhaseAvailable Phase = "available"
	// PhaseDownloading means the package is being downloaded.
	PhaseDownloading Phase = "downloading"
	// PhaseInstalling means the package is being put in place.
	PhaseInstalling Phase = "installing"
	// PhaseRestarting means the new version is in place and the program
	// is quitting to start it.
	PhaseRestarting Phase = "restarting"
)

// State is what the dashboard shows of the updater.
type State struct {
	// Version is the installed version.
	Version string `json:"version"`
	Phase   Phase  `json:"phase"`
	// Release is the newer release while one is on offer or installing.
	Release *Release `json:"release,omitempty"`
	// Received and Total are the bytes of the package downloaded so far
	// and in all, while downloading.
	Received int64 `json:"received"`
	Total    int64 `json:"total"`
	// CheckedAt is when the last check finished, in Unix milliseconds;
	// zero before the first.
	CheckedAt int64 `json:"checkedAt"`
	// Error is why the last check or installation failed.
	Error string `json:"error,omitempty"`
	// Leftover is why what an earlier update left behind could not be
	// removed.
	Leftover string `json:"leftover,omitempty"`
	// Unavailable says why this copy cannot update itself; empty when it
	// can.
	Unavailable string `json:"unavailable,omitempty"`
	// Prereleases is whether alpha and beta versions are offered too.
	Prereleases bool `json:"prereleases"`
	// Elevation reports that installing also updates the Windows file
	// index service, which asks for administrator rights.
	Elevation bool `json:"elevation"`
}

// Store keeps the updater's setting. config.Store satisfies it.
type Store interface {
	UpdatePrereleases() bool
	SetUpdatePrereleases(on bool) error
}

// Updater finds, downloads and installs new releases.
type Updater struct {
	store     Store
	log       *slog.Logger
	installed version
	client    *http.Client
	app       *application.App
	changed   func(State)
	// ctx ends with the program, which gives up a check or a download.
	ctx    context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	state State
	offer *offer
}

// New creates the updater for the installed version. It does nothing
// until Start.
func New(store Store, log *slog.Logger, installed string) (*Updater, error) {
	u := &Updater{
		store: store,
		log:   log,
		client: &http.Client{
			// Downloads end on a stall instead, as a package takes a while.
			Transport: &http.Transport{
				Proxy:                 platform.SystemProxy,
				ForceAttemptHTTP2:     true,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
		state: State{Version: installed, Phase: PhaseIdle, Prereleases: store.UpdatePrereleases()},
	}
	u.state.Unavailable = unavailable()
	if u.state.Unavailable != "" {
		return u, nil
	}
	v, err := parseVersion(installed)
	if err != nil {
		return nil, fmt.Errorf("installed version: %w", err)
	}
	u.installed = v
	return u, nil
}

// Service is what the dashboard calls.
func (u *Updater) Service() application.Service {
	return application.NewService(&Service{updater: u})
}

// Start removes what an earlier update left behind and checks for a new
// release now and every few hours. changed is called on every change of
// the state.
func (u *Updater) Start(app *application.App, changed func(State)) {
	u.app, u.changed = app, changed
	if u.state.Unavailable != "" {
		return
	}
	u.ctx, u.cancel = context.WithCancel(context.Background())
	go func() {
		if err := cleanUp(); err != nil {
			u.log.Error("remove what the last update left", "error", err)
			u.update(func(s *State) { s.Leftover = err.Error() })
		}
		t := time.NewTicker(checkEvery)
		defer t.Stop()
		for {
			if _, err := u.check(); err != nil && u.ctx.Err() == nil {
				u.log.Warn("check for updates", "error", err)
			}
			select {
			case <-u.ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Stop ends the periodic checks and gives up a running check or
// download. It does not wait for them: their last report reaches the tray
// through the UI thread, which the program may be quitting on.
func (u *Updater) Stop() {
	if u.cancel != nil {
		u.cancel()
	}
}

// State returns the current state.
func (u *Updater) State() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

// update changes the state and reports it.
func (u *Updater) update(fn func(s *State)) State {
	u.mu.Lock()
	fn(&u.state)
	s := u.state
	u.mu.Unlock()
	u.app.Event.Emit(stateEvent, s)
	u.changed(s)
	return s
}

// errBusy is returned for a check or an installation while another runs.
var errBusy = errors.New("正在检查或安装更新")

// busy reports whether a check or an installation runs. u.mu is held.
func (u *Updater) busy() bool {
	switch u.state.Phase {
	case PhaseChecking, PhaseDownloading, PhaseInstalling, PhaseRestarting:
		return true
	}
	return false
}

// check looks for a newer release.
func (u *Updater) check() (State, error) {
	u.mu.Lock()
	if u.state.Unavailable != "" {
		s := u.state
		u.mu.Unlock()
		return s, errors.New(s.Unavailable)
	}
	if u.busy() {
		s := u.state
		u.mu.Unlock()
		return s, errBusy
	}
	previous := u.state.Phase
	prereleases := u.state.Prereleases
	u.state.Phase = PhaseChecking
	u.mu.Unlock()
	u.update(func(s *State) { s.Error = "" })

	ctx, cancel := context.WithTimeout(u.ctx, time.Minute)
	defer cancel()
	o, err := newest(ctx, u.client, u.installed, prereleases)
	var elevation bool
	if err == nil && o != nil {
		elevation, err = needsElevation()
	}
	if err != nil {
		err = fmt.Errorf("检查更新失败：%w", err)
		return u.update(func(s *State) {
			// A release found earlier stays on offer.
			s.Phase, s.Error = PhaseIdle, err.Error()
			if previous == PhaseAvailable {
				s.Phase = PhaseAvailable
			}
		}), err
	}
	u.mu.Lock()
	u.offer = o
	u.mu.Unlock()
	return u.update(func(s *State) {
		s.CheckedAt = time.Now().UnixMilli()
		s.Elevation = elevation
		if o == nil {
			s.Phase, s.Release = PhaseLatest, nil
			return
		}
		s.Phase, s.Release = PhaseAvailable, &o.Release
	}), nil
}

// install downloads the release on offer, puts it in place and restarts
// the program. A failure is also left in the state.
func (u *Updater) install() error {
	u.mu.Lock()
	if u.busy() {
		u.mu.Unlock()
		return errBusy
	}
	o := u.offer
	if u.state.Phase != PhaseAvailable || o == nil {
		u.mu.Unlock()
		err := errors.New("没有可安装的更新")
		u.update(func(s *State) { s.Error = err.Error() })
		return err
	}
	u.state.Phase = PhaseDownloading
	u.mu.Unlock()
	u.update(func(s *State) { s.Error, s.Received, s.Total = "", 0, o.file.Size })

	var reported time.Time
	progress := func(received int64) {
		// The page needs no more than a few updates a second.
		if received < o.file.Size && time.Since(reported) < 100*time.Millisecond {
			return
		}
		reported = time.Now()
		u.update(func(s *State) { s.Received = received })
	}
	installing := func() { u.update(func(s *State) { s.Phase = PhaseInstalling }) }
	relaunch, err := put(u.ctx, u.client, o.file, progress, installing)
	if err != nil {
		u.log.Error("install update", "version", o.Version, "error", err)
		u.update(func(s *State) { s.Phase, s.Error = PhaseAvailable, "安装更新失败："+err.Error() })
		return err
	}
	u.log.Info("update installed, restarting", "version", o.Version)
	u.update(func(s *State) { s.Phase = PhaseRestarting })
	if err := relaunch(); err != nil {
		// The new version is in place but did not start; it starts the
		// next time the program is opened.
		u.log.Error("restart after update", "error", err)
		u.update(func(s *State) {
			s.Error = "新版本已安装，但没能自动重启：" + err.Error() + "。请手动退出并重新打开。"
		})
		return err
	}
	u.app.Quit()
	return nil
}

// setPrereleases changes whether prereleases are offered and checks again.
// A release found before is no longer on offer, as it may be a prerelease.
func (u *Updater) setPrereleases(on bool) (State, error) {
	u.mu.Lock()
	if u.busy() {
		s := u.state
		u.mu.Unlock()
		return s, errBusy
	}
	if err := u.store.SetUpdatePrereleases(on); err != nil {
		s := u.state
		u.mu.Unlock()
		return s, err
	}
	u.offer = nil
	unavailable := u.state.Unavailable != ""
	u.mu.Unlock()
	s := u.update(func(s *State) {
		s.Prereleases, s.Release = on, nil
		if s.Phase == PhaseAvailable || s.Phase == PhaseLatest {
			s.Phase = PhaseIdle
		}
	})
	if unavailable {
		return s, nil
	}
	// A failed check is in the state.
	s, _ = u.check()
	return s, nil
}

// Service is the dashboard's view of the updater.
type Service struct {
	updater *Updater
}

// State returns the current state.
func (s *Service) State() State { return s.updater.State() }

// Check looks for a newer release now. The state it returns tells how it
// went, a failure included.
func (s *Service) Check() State {
	st, _ := s.updater.check()
	return st
}

// Install downloads and installs the release on offer, then restarts the
// program. Progress and failures arrive as update:state events.
func (s *Service) Install() {
	_ = s.updater.install()
}

// SetPrereleases sets whether alpha and beta versions are offered, and
// checks again.
func (s *Service) SetPrereleases(on bool) (State, error) { return s.updater.setPrereleases(on) }
