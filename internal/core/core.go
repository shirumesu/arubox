// Package core is the main program: it wires settings, logging, hotkeys,
// modules, the tray icon and the dashboard together.
package core

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/shirumesu/arubox/internal/config"
	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/logging"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/modules/desktop"
	"github.com/shirumesu/arubox/internal/modules/launcher"
	"github.com/shirumesu/arubox/internal/modules/screenshot"
	"github.com/shirumesu/arubox/internal/modules/translate"
	"github.com/shirumesu/arubox/internal/ocr"
	"github.com/shirumesu/arubox/internal/ui"
	"github.com/shirumesu/arubox/internal/update"
)

// Version is set at build time.
var Version = "dev"

const (
	appName  = "arubox"
	bundleID = "com.shirumesu.arubox"
	// coreOwner owns the hotkeys of the main program itself.
	coreOwner = "core"
	// gcPercent lets the heap grow only a tenth past what is live before
	// it is collected, not double. Most of what is live is the file index,
	// which holds no pointers, so a collection costs little however large
	// it is, and more room would keep megabytes unused.
	gcPercent = 10
	// settleAfter is when start-up is over: loading the file index's
	// snapshot, listing volumes and reading the app list leave tens of
	// megabytes of garbage, which Go would hand back over minutes.
	settleAfter = 20 * time.Second
)

// Core holds the running program. Its fields are set before the UI thread
// starts and not changed after.
type Core struct {
	log       *slog.Logger
	logDir    string
	store     *config.Store
	hotkeys   *hotkey.Manager
	modules   *module.Registry
	updater   *update.Updater
	reader    *ocr.Engine
	dashboard *dashboard
	tray      *ui.Tray
	// trayUpdate is the tray menu's update item label, "" while none is on
	// offer. Touched on the UI thread only.
	trayUpdate string
}

// Run starts the program and blocks until it quits.
func Run() error {
	debug.SetGCPercent(gcPercent)
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
	dataDir, err := dataDirectory()
	if err != nil {
		return err
	}
	updater, err := update.New(store, log, Version)
	if err != nil {
		return err
	}
	c := &Core{log: log, logDir: logDir, store: store, updater: updater}
	c.reader = ocr.New(store, log, filepath.Join(dataDir, "ocr"))
	translator := translate.New(store, log, c.reader)
	modules := []module.Module{
		translator,
		launcher.New(store, log, launcherTranslator{translator}),
		screenshot.New(store, log, c.reader, screenshotTranslator{translator}),
		desktop.New(store, log, filepath.Join(configDir, appName, "desktop-snapshots")),
	}
	c.hotkeys = hotkey.NewManager(hotkey.NewSystem(), store, log)
	c.hotkeys.Define(coreOwner, appName, []hotkey.Def{
		{ID: "dashboard", Label: "打开仪表盘", Action: func() { ui.Post(c.dashboard.open) }},
	})
	c.modules = module.NewRegistry(module.Context{Log: log}, store, c.hotkeys, modules)

	err = ui.Run(ui.Options{ID: bundleID, OnSecondInstance: func() { c.dashboard.open() }, Log: log}, func() {
		c.dashboard = newDashboard(c)
		c.setupTray()
		c.hotkeys.Activate(coreOwner)
		c.modules.Start()
		c.updater.OnChange(func(s update.State) { ui.Post(func() { c.updateTray(s) }) })
		c.updater.Start(ui.Quit)
		ui.OnQuit(c.shutdown)
		time.AfterFunc(settleAfter, debug.FreeOSMemory)
	})
	if errors.Is(err, ui.ErrRunning) {
		log.Info("already running; asked it to show the dashboard")
		return nil
	}
	if err != nil {
		log.Error("stopped with error", "error", err)
		return err
	}
	log.Info("stopped")
	return nil
}

// shutdown runs on the UI thread as the program quits.
func (c *Core) shutdown() {
	c.updater.Stop()
	c.modules.Stop()
	c.hotkeys.UnregisterAll()
	if c.tray != nil {
		c.tray.Remove()
	}
}
