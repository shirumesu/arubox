package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/ui"
	"github.com/shirumesu/arubox/internal/update"
)

// generalPage holds starting at login, updates and the logs.
type generalPage struct{ d *dashboard }

func (p generalPage) Draw(f *ui.Frame, x, y, w float64) float64 {
	d := p.d
	y = f.Section(x, y, w, "启动", []ui.Row{{
		Title: "登录时启动", Description: "登录系统后自动在后台运行。", Note: d.autostartErr,
		ControlW: 36,
		Control: func(r ui.Rect) {
			on := d.autostart
			if f.Switch("general.autostart", r, &on, false) {
				d.setAutostart(on)
			}
		},
	}})
	y += sectionGap
	y = p.updates(f, x, y, w)
	y += sectionGap
	logs := ui.Button{Label: "打开日志目录", Icon: ui.IconFolder, Small: true}
	return f.Section(x, y, w, "故障排查", []ui.Row{{
		Title: "日志", Description: "运行记录保存在日志目录中，出问题时可以从这里查看。",
		ControlW: logs.Width(),
		Control: func(r ui.Rect) {
			if f.Button("general.logs", r, logs) {
				d.fail(ui.OpenURL(d.core.logDir))
			}
		},
	}})
}

func (p generalPage) updates(f *ui.Frame, x, y, w float64) float64 {
	d := p.d
	s := d.update
	var notes []string
	for _, n := range []string{s.Error, s.Leftover} {
		if n != "" {
			notes = append(notes, n)
		}
	}
	row := ui.Row{Title: "arubox " + s.Version, Description: updateStatus(s), Note: strings.Join(notes, "；")}
	switch {
	case s.Unavailable != "":
	case offered(s.Phase):
		b := ui.Button{Label: "查看新版本", Kind: ui.ButtonPrimary, Small: true}
		row.ControlW = b.Width()
		row.Control = func(r ui.Rect) {
			if f.Button("update.show", r, b) {
				d.showUpdate = true
			}
		}
	default:
		b := ui.Button{Label: "检查更新", Icon: ui.IconRefresh, Small: true, Disabled: s.Phase == update.PhaseChecking}
		row.ControlW = b.Width()
		row.Control = func(r ui.Rect) {
			if f.Button("update.check", r, b) {
				// A check waits for GitHub; its outcome arrives through
				// the updater's OnChange.
				go d.core.updater.Check()
			}
		}
	}
	return f.Section(x, y, w, "更新", []ui.Row{
		row,
		{
			Title:       "接收测试版",
			Description: "除了正式版，也提示 alpha、beta 等测试版本。测试版更早带来新功能，但可能不够稳定。",
			ControlW:    36,
			Control: func(r ui.Rect) {
				on := s.Prereleases
				if f.Switch("update.prereleases", r, &on, s.Unavailable != "") {
					go func() {
						if _, err := d.core.updater.SetPrereleases(on); err != nil {
							ui.Post(func() { d.fail(err) })
						}
					}()
				}
			},
		},
	})
}

// offered reports whether a release is on offer or being installed.
func offered(p update.Phase) bool {
	switch p {
	case update.PhaseAvailable, update.PhaseDownloading, update.PhaseInstalling, update.PhaseRestarting:
		return true
	}
	return false
}

func updateStatus(s update.State) string {
	if s.Unavailable != "" {
		return s.Unavailable
	}
	release := ""
	if s.Release != nil {
		release = s.Release.Version
	}
	switch s.Phase {
	case update.PhaseChecking:
		return "正在检查更新…"
	case update.PhaseLatest:
		return "已是最新版本（" + checkedAt(s.CheckedAt) + " 检查）"
	case update.PhaseAvailable:
		return "有新版本 " + release
	case update.PhaseDownloading:
		return "正在下载 " + release + "…"
	case update.PhaseInstalling:
		return "正在安装 " + release + "…"
	case update.PhaseRestarting:
		return release + " 已安装，正在重启…"
	}
	return "启动时和之后每隔几小时自动检查一次。"
}

// checkedAt tells when a check finished, as "今天 14:02" or "11月10日 14:02".
func checkedAt(ms int64) string {
	at := time.UnixMilli(ms).Local()
	now := time.Now()
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return "今天 " + at.Format("15:04")
	}
	return fmt.Sprintf("%d月%d日 %s", at.Month(), at.Day(), at.Format("15:04"))
}

// modulesPage turns the modules on and off.
type modulesPage struct{ d *dashboard }

func (p modulesPage) Draw(f *ui.Frame, x, y, w float64) float64 {
	d := p.d
	if len(d.modules) == 0 {
		return f.Empty(x, y, w, ui.IconApps, "还没有模块", "各项功能会列在这里，可以分别开关。关掉的功能不占内存，快捷键也不生效。")
	}
	rows := make([]ui.Row, len(d.modules))
	for i, m := range d.modules {
		rows[i] = moduleRow(f, d, m)
	}
	return f.Section(x, y, w, "全部模块", rows)
}

func moduleRow(f *ui.Frame, d *dashboard, m module.Status) ui.Row {
	return ui.Row{
		Title: m.Name, Description: m.Description, Note: m.Error,
		ControlW: 36,
		Control: func(r ui.Rect) {
			on := m.Enabled
			if f.Switch("modules."+m.ID, r, &on, false) {
				d.setModuleEnabled(m.ID, on)
			}
		},
	}
}
