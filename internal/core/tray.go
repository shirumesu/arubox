package core

import (
	_ "embed"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
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

func setupTray(c *Core) {
	tray := c.app.SystemTray.New()
	tray.SetTooltip(appName)

	menu := application.NewMenu()
	menu.Add("打开仪表盘").OnClick(func(*application.Context) { c.dashboard.open() })
	menu.AddSeparator()
	menu.Add("退出 " + appName).OnClick(func(*application.Context) { c.app.Quit() })
	tray.SetMenu(menu)

	if runtime.GOOS == "darwin" {
		// A click opens the menu, as with every menu bar item.
		tray.SetTemplateIcon(trayTemplate)
		return
	}
	// On Windows a left click opens the dashboard and a right click the menu.
	tray.SetIcon(trayColour)
	tray.OnClick(c.dashboard.open)
}
