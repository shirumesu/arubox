package core

import (
	_ "embed"

	"github.com/shirumesu/arubox/internal/ui"
	"github.com/shirumesu/arubox/internal/update"
)

// trayIcon is the notification area icon.
//
//go:embed assets/tray.png
var trayIcon []byte

// appIcon is the program's icon, for the dashboard's title bar.
//
//go:embed assets/app.ico
var appIcon []byte

// setupTray adds the tray icon: a left click opens the dashboard, a right
// click the menu. Call on the UI thread.
func (c *Core) setupTray() {
	t := ui.NewTray(trayIcon, appName)
	t.OnClick = c.dashboard.open
	t.Menu = func() []ui.MenuItem {
		var items []ui.MenuItem
		if c.trayUpdate != "" {
			items = append(items, ui.MenuItem{Label: c.trayUpdate, Action: c.dashboard.openUpdate}, ui.MenuItem{})
		}
		return append(items,
			ui.MenuItem{Label: "打开仪表盘", Action: c.dashboard.open},
			ui.MenuItem{},
			ui.MenuItem{Label: "退出 " + appName, Action: ui.Quit},
		)
	}
	c.tray = t
}

// updateTray shows a menu item for a new release while one is on offer.
// Call on the UI thread.
func (c *Core) updateTray(s update.State) {
	switch s.Phase {
	case update.PhaseAvailable, update.PhaseDownloading, update.PhaseInstalling:
		c.trayUpdate = "更新到 " + s.Release.Version + "…"
	default:
		c.trayUpdate = ""
	}
}
