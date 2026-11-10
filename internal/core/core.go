// Package core is the main program: it wires settings, logging, hotkeys,
// modules, the tray icon and the dashboard together.
package core

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shirumesu/arubox/internal/config"
	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/logging"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/modules/desktop"
	"github.com/shirumesu/arubox/internal/modules/launcher"
	"github.com/shirumesu/arubox/internal/modules/screenshot"
	"github.com/shirumesu/arubox/internal/modules/translate"
)

// Version is set at build time.
var Version = "dev"

const (
	appName  = "arubox"
	bundleID = "com.shirumesu.arubox"
	// coreOwner owns the hotkeys of the main program itself.
	coreOwner = "core"
	// gcPercent lets the heap grow only a quarter past what is live before
	// it is collected, not double. Most of what is live is the file index,
	// which holds no pointers, so a collection costs little however large
	// it is, and doubling it would keep tens of megabytes unused.
	gcPercent = 25
)

// Core holds the running program.
type Core struct {
	app       *application.App
	log       *slog.Logger
	logDir    string
	hotkeys   *hotkey.Manager
	modules   *module.Registry
	dashboard *dashboard
}

// Run starts the program and blocks until it quits. assets is the built
// frontend and css its stylesheet, which holds the design tokens.
func Run(assets fs.FS, css []byte) error {
	debug.SetGCPercent(gcPercent)
	tokens, err := design.Parse(css)
	if err != nil {
		return err
	}
	logDir, err := logDirectory()
	if err != nil {
		return err
	}
	logOut, closeLog, err := logging.Open(logDir)
	if err != nil {
		return err
	}
	defer closeLog()
	log := logging.New(logOut, slog.LevelInfo)
	log.Info("starting", "version", Version, "pid", os.Getpid())

	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("locate settings directory: %w", err)
	}
	store, err := config.Open(filepath.Join(configDir, appName, "config.json"), log)
	if err != nil {
		return err
	}

	c := &Core{log: log, logDir: logDir}
	translator := translate.New(store, log)
	modules := []module.Module{
		translator,
		launcher.New(store, log, launcherTranslator{translator}),
		screenshot.New(store, log, screenshotTranslator{translator}),
		desktop.New(store, log, tokens),
	}
	services := []application.Service{application.NewService(&DashboardService{core: c})}
	for _, m := range modules {
		services = append(services, m.Services()...)
	}
	c.app = application.New(application.Options{
		Name:        appName,
		Description: "个人工具箱",
		// Wails logs every asset request at info level; keep only problems.
		Logger:   logging.New(logOut, slog.LevelWarn),
		Services: services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		Windows: application.WindowsOptions{
			// Every window shows a page of the same site; one process
			// renders them all, instead of one each.
			AdditionalBrowserArgs: []string{"--process-per-site"},
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: bundleID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				c.dashboard.open()
			},
		},
		OnShutdown: c.shutdown,
	})
	c.dashboard = &dashboard{core: c}
	c.hotkeys = hotkey.NewManager(c.app.GlobalShortcut, store, log)
	c.hotkeys.Define(coreOwner, appName, []hotkey.Def{
		{ID: "dashboard", Label: "打开仪表盘", Action: c.dashboard.open},
	})
	c.modules = module.NewRegistry(module.Context{App: c.app, Log: log}, store, c.hotkeys, modules)

	setupTray(c)
	c.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		c.hotkeys.Activate(coreOwner)
		c.modules.Start()
	})

	if err := c.app.Run(); err != nil {
		log.Error("application stopped with error", "error", err)
		return err
	}
	log.Info("stopped")
	return nil
}

func (c *Core) shutdown() {
	c.modules.Stop()
	c.hotkeys.UnregisterAll()
}
