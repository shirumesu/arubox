package core

import (
	_ "embed"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/update"
)

// trayTemplate is the macOS menu bar icon. It is a template image: macOS
// recolours it for light and dark menu bars.
//
//go:embed assets/tray-template.png
var trayTemplate []byte

// trayColour is the Windows notification area icon.
//
//go:embed assets/tray.png
var trayColour []byte

// setupTray adds the tray icon and its menu. The returned function shows
// a menu item for a new release while one is on offer.
func setupTray(c *Core) func(update.State) {
	tray := c.app.SystemTray.New()
	tray.SetTooltip(appName)

	// Tray callbacks run on the UI thread. On Windows, creating the dashboard
	// window pumps messages while WebView2 starts, so a second click arriving
	// then would call open again on the same thread and deadlock on its lock.
	// Opening from a goroutine leaves the UI thread free.
	openDashboard := func() { go c.dashboard.open() }

	menu := application.NewMenu()
	updateItem := menu.Add("").SetHidden(true)
	updateItem.OnClick(func(*application.Context) { go c.dashboard.openUpdate() })
	menu.Add("打开仪表盘").OnClick(func(*application.Context) { openDashboard() })
	menu.AddSeparator()
	menu.Add("退出 " + appName).OnClick(func(*application.Context) { c.app.Quit() })
	tray.SetMenu(menu)

	if runtime.GOOS == "darwin" {
		// A click opens the menu, as with every menu bar item.
		tray.SetTemplateIcon(trayTemplate)
	} else {
		// On Windows a left click opens the dashboard and a right click the menu.
		tray.SetIcon(trayColour)
		tray.OnClick(openDashboard)
	}

	// The label shown, touched only on the UI thread; empty while hidden.
	// Downloads report many times a second, mostly with no change here.
	shown := ""
	return func(s update.State) {
		label := ""
		switch s.Phase {
		case update.PhaseAvailable, update.PhaseDownloading, update.PhaseInstalling:
			label = "更新到 " + s.Release.Version + "…"
		}
		application.InvokeAsync(func() {
			if label == shown {
				return
			}
			shown = label
			if label != "" {
				updateItem.SetLabel(label)
			}
			updateItem.SetHidden(label == "")
			// Windows builds the tray menu without hidden items, so it is
			// built again.
			tray.SetMenu(menu)
		})
	}
}
