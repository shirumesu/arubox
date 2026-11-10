// Package desktop is the desktop module: it takes over the desktop's icons
// and keeps them in boxes drawn on the desktop, the way Fences and iTop
// Easy Desktop do. The files stay in the desktop folders; a box only
// remembers which items it holds.
package desktop

import (
	"errors"
	"log/slog"
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/module"
)

const id = "desktop"

// Store keeps the module's settings. config.Store satisfies it.
type Store interface {
	ModuleSettings(id string, v any) error
	SetModuleSettings(id string, v any) error
}

// Settings is what the user can change on the settings page.
type Settings struct {
	// IconSize is the size of icons, in DIPs.
	IconSize int `json:"iconSize"`
	// Opacity is how opaque a box's background is, in percent.
	Opacity int `json:"opacity"`
	// DoubleClickHide hides and shows every icon and box on a double-click
	// on the empty desktop.
	DoubleClickHide bool `json:"doubleClickHide"`
	// Snap lines boxes up with the screen's edges and with each other.
	Snap bool `json:"snap"`
	// HoverExpand opens a rolled-up box while the pointer is over it.
	HoverExpand bool `json:"hoverExpand"`
}

// stored is what the module keeps in the settings file.
type stored struct {
	Settings
	Layout Layout `json:"layout"`
}

func defaultStored() stored {
	return stored{Settings: Settings{IconSize: 48, Opacity: 45, DoubleClickHide: true, Snap: true, HoverExpand: true}}
}

// Icon sizes and opacities the settings accept.
const (
	minIconSize = 32
	maxIconSize = 96
	minOpacity  = 10
	maxOpacity  = 90
)

// Module is the desktop module.
type Module struct {
	store  Store
	log    *slog.Logger
	tokens design.Tokens

	mu     sync.Mutex
	stored stored
	view   *view // nil while disabled
}

// New creates the module. It does nothing until enabled.
func New(store Store, log *slog.Logger, tokens design.Tokens) *Module {
	s := defaultStored()
	if err := store.ModuleSettings(id, &s); err != nil {
		log.Warn("desktop settings unreadable, using defaults", "error", err)
		s = defaultStored()
	}
	return &Module{store: store, log: log, tokens: tokens, stored: s}
}

// Info describes the module.
func (m *Module) Info() module.Info {
	return module.Info{
		ID:          id,
		Name:        "桌面整理",
		Description: "在桌面空白处拖出一个框来建盒子，把图标归进盒子；也能建文件夹入口，直接在桌面上浏览一个文件夹。文件仍在桌面文件夹里。",
		Platforms:   []string{"windows"},
	}
}

// Services binds the settings page.
func (m *Module) Services() []application.Service {
	return []application.Service{application.NewService(&Service{module: m})}
}

// Hotkeys declares none: boxes are made by dragging on the desktop.
func (m *Module) Hotkeys() []hotkey.Def { return nil }

// Enable takes over the desktop.
func (m *Module) Enable(module.Context) error {
	m.mu.Lock()
	s := m.stored
	m.mu.Unlock()
	v, err := startView(m, s)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.view = v
	m.mu.Unlock()
	return nil
}

// Disable gives the desktop back to File Explorer.
func (m *Module) Disable() error {
	m.mu.Lock()
	v := m.view
	m.view = nil
	m.mu.Unlock()
	if v != nil {
		v.stop()
	}
	return nil
}

// saveLayout keeps the layout; the view calls it on its own thread.
func (m *Module) saveLayout(l Layout) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.stored
	next.Layout = l
	if err := m.store.SetModuleSettings(id, next); err != nil {
		m.log.Warn("save desktop layout", "error", err)
		return
	}
	m.stored = next
}

func (m *Module) currentSettings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stored.Settings
}

func (m *Module) setSettings(s Settings) error {
	if s.IconSize < minIconSize || s.IconSize > maxIconSize {
		return errors.New("图标大小超出范围")
	}
	if s.Opacity < minOpacity || s.Opacity > maxOpacity {
		return errors.New("不透明度超出范围")
	}
	m.mu.Lock()
	next := m.stored
	next.Settings = s
	if err := m.store.SetModuleSettings(id, next); err != nil {
		m.mu.Unlock()
		return errors.New("保存设置失败：" + err.Error())
	}
	m.stored = next
	v := m.view
	m.mu.Unlock()
	if v != nil {
		v.setSettings(s)
	}
	return nil
}

// Service is what the dashboard's settings page calls.
type Service struct {
	module *Module
}

// SettingsState is everything the settings page shows.
type SettingsState struct {
	Settings Settings `json:"settings"`
	// Platform is the GOOS value.
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
