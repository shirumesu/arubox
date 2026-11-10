package hotkey

// windowsShortcuts are shortcuts Windows or every app already uses. Most of
// the shell's own Win+ shortcuts also make RegisterHotKey fail, but some are
// handled by a keyboard hook, where registration succeeds and the hotkey
// silently never fires, so they are listed here too.
var windowsShortcuts = map[string]string{
	"Alt+tab":           "切换窗口",
	"Alt+Shift+tab":     "反向切换窗口",
	"Alt+escape":        "循环切换窗口",
	"Alt+f4":            "关闭窗口",
	"Alt+space":         "窗口菜单",
	"Ctrl+escape":       "开始菜单",
	"Ctrl+Shift+escape": "任务管理器",
	"Ctrl+Alt+delete":   "安全选项",
	"Ctrl+space":        "切换输入法中英文",
	"Ctrl+Shift+space":  "切换全角半角",
	"Super+a":           "快速设置",
	"Super+d":           "显示桌面",
	"Super+e":           "文件资源管理器",
	"Super+i":           "设置",
	"Super+k":           "投放",
	"Super+l":           "锁定",
	"Super+m":           "最小化所有窗口",
	"Super+n":           "通知中心",
	"Super+p":           "投影",
	"Super+r":           "运行",
	"Super+s":           "搜索",
	"Super+v":           "剪贴板历史",
	"Super+x":           "快速链接菜单",
	"Super+tab":         "任务视图",
	"Super+space":       "切换输入法",
	"Super+.":           "表情符号",
	"Super+;":           "表情符号",
	"Shift+Super+s":     "截图",
	"Super+left":        "窗口贴靠左侧",
	"Super+right":       "窗口贴靠右侧",
	"Super+up":          "最大化窗口",
	"Super+down":        "还原或最小化窗口",
	"Ctrl+a":            "全选",
	"Ctrl+c":            "复制",
	"Ctrl+v":            "粘贴",
	"Ctrl+x":            "剪切",
	"Ctrl+z":            "撤销",
	"Ctrl+y":            "重做",
	"Ctrl+s":            "保存",
	"Ctrl+f":            "查找",
	"Ctrl+n":            "新建",
	"Ctrl+t":            "新建标签页",
	"Ctrl+o":            "打开",
	"Ctrl+p":            "打印",
	"Ctrl+w":            "关闭标签页",
	"Ctrl+tab":          "切换标签页",
	"Ctrl+Shift+tab":    "反向切换标签页",
}

// systemShortcuts returns the shortcuts Windows or every app already uses,
// keyed by canonical accelerator.
func systemShortcuts() map[string]string {
	return windowsShortcuts
}
