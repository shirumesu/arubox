package hotkey

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"time"

	"howett.net/plist"
)

// macOS reports symbolic hotkeys with Cocoa modifier flags.
const (
	nsShift   = 1 << 17
	nsControl = 1 << 18
	nsOption  = 1 << 19
	nsCommand = 1 << 20
)

// macKeyNames maps macOS virtual key codes (kVK_*, ANSI positions) to key
// names, for the keys a hotkey may use.
var macKeyNames = map[int]string{
	0: "a", 11: "b", 8: "c", 2: "d", 14: "e", 3: "f", 5: "g", 4: "h", 34: "i",
	38: "j", 40: "k", 37: "l", 46: "m", 45: "n", 31: "o", 35: "p", 12: "q",
	15: "r", 1: "s", 17: "t", 32: "u", 9: "v", 13: "w", 7: "x", 16: "y", 6: "z",
	29: "0", 18: "1", 19: "2", 20: "3", 21: "4", 23: "5", 22: "6", 26: "7", 28: "8", 25: "9",
	27: "-", 24: "=", 33: "[", 30: "]", 42: "\\", 41: ";", 39: "'", 43: ",", 47: ".", 44: "/", 50: "`",
	49: "space", 36: "return", 48: "tab", 51: "backspace", 117: "delete", 53: "escape",
	115: "home", 119: "end", 116: "page up", 121: "page down",
	123: "left", 124: "right", 126: "up", 125: "down",
	122: "f1", 120: "f2", 99: "f3", 118: "f4", 96: "f5", 97: "f6", 98: "f7", 100: "f8",
	101: "f9", 109: "f10", 103: "f11", 111: "f12", 105: "f13", 107: "f14", 113: "f15",
	106: "f16", 64: "f17", 79: "f18", 80: "f19", 90: "f20",
}

// symbolicHotkey is a shortcut from System Settings > Keyboard > Keyboard
// Shortcuts, identified by its id in com.apple.symbolichotkeys.
type symbolicHotkey struct {
	name    string
	keyCode int
	flags   int
	enabled bool
}

// macSymbolicDefaults are the factory settings. The preferences domain only
// stores entries the user changed, so these fill in the rest.
var macSymbolicDefaults = map[int]symbolicHotkey{
	7:   {"将焦点移到菜单栏", 120, nsControl, true},
	8:   {"将焦点移到程序坞", 99, nsControl, true},
	9:   {"将焦点移到活跃或下一个窗口", 118, nsControl, true},
	10:  {"将焦点移到窗口工具栏", 96, nsControl, true},
	11:  {"将焦点移到浮动窗口", 97, nsControl, true},
	12:  {"打开或关闭键盘导航", 122, nsControl, true},
	13:  {"更改 Tab 键移动焦点的方式", 98, nsControl, true},
	27:  {"将焦点移到下一个窗口", 50, nsCommand, true},
	28:  {"将屏幕快照存储为文件", 20, nsCommand | nsShift, true},
	29:  {"将屏幕快照拷贝到剪贴板", 20, nsCommand | nsShift | nsControl, true},
	30:  {"将所选区域的快照存储为文件", 21, nsCommand | nsShift, true},
	31:  {"将所选区域的快照拷贝到剪贴板", 21, nsCommand | nsShift | nsControl, true},
	32:  {"调度中心", 126, nsControl, true},
	33:  {"应用程序窗口", 125, nsControl, true},
	36:  {"显示桌面", 103, 0, true},
	52:  {"打开或关闭程序坞隐藏", 2, nsCommand | nsOption, true},
	57:  {"将焦点移到状态菜单", 100, nsControl, true},
	60:  {"选择上一个输入法", 49, nsControl, true},
	61:  {"选择输入法菜单中的下一个输入法", 49, nsControl | nsOption, true},
	64:  {"显示聚焦搜索", 49, nsCommand, true},
	65:  {"显示“访达”搜索窗口", 49, nsCommand | nsOption, true},
	79:  {"向左移动一个空间", 123, nsControl, true},
	81:  {"向右移动一个空间", 124, nsControl, true},
	98:  {"显示“帮助”菜单", 44, nsCommand | nsShift, true},
	118: {"切换到桌面 1", 18, nsControl, false},
	119: {"切换到桌面 2", 19, nsControl, false},
	120: {"切换到桌面 3", 20, nsControl, false},
	121: {"切换到桌面 4", 21, nsControl, false},
	162: {"显示辅助功能控制", 96, nsCommand | nsOption, true},
	184: {"截屏和录制选项", 23, nsCommand | nsShift, true},
}

// macFixedShortcuts are system shortcuts that cannot be changed in settings.
var macFixedShortcuts = map[string]string{
	"Super+tab":              "切换应用",
	"Shift+Super+tab":        "反向切换应用",
	"Alt+Super+escape":       "强制退出应用",
	"Ctrl+Super+q":           "锁定屏幕",
	"Shift+Super+q":          "退出登录",
	"Ctrl+Super+space":       "表情与符号",
	"Ctrl+Super+f":           "全屏",
	"Super+h":                "隐藏应用",
	"Alt+Super+h":            "隐藏其他应用",
	"Super+m":                "最小化窗口",
	"Super+q":                "退出应用",
	"Super+w":                "关闭窗口",
	"Super+a":                "全选",
	"Super+c":                "拷贝",
	"Super+v":                "粘贴",
	"Super+x":                "剪切",
	"Super+z":                "撤销",
	"Shift+Super+z":          "重做",
	"Super+s":                "存储",
	"Super+f":                "查找",
	"Super+n":                "新建",
	"Super+t":                "新建标签页",
	"Super+o":                "打开",
	"Super+p":                "打印",
	"Super+,":                "应用设置",
	"Alt+Shift+Super+escape": "强制退出前台应用",
}

// systemShortcuts returns the shortcuts macOS or every app already uses,
// keyed by canonical accelerator.
func systemShortcuts() map[string]string {
	result := make(map[string]string, len(macFixedShortcuts)+len(macSymbolicDefaults))
	for accel, name := range macFixedShortcuts {
		result[accel] = name
	}
	for _, hk := range effectiveSymbolicHotkeys() {
		if !hk.enabled {
			continue
		}
		if accel, ok := hk.accelerator(); ok {
			result[accel] = hk.name
		}
	}
	return result
}

// effectiveSymbolicHotkeys merges the user's changes over the defaults.
func effectiveSymbolicHotkeys() map[int]symbolicHotkey {
	result := make(map[int]symbolicHotkey, len(macSymbolicDefaults))
	for id, hk := range macSymbolicDefaults {
		result[id] = hk
	}
	for id, entry := range readSymbolicHotkeys() {
		hk, known := result[id]
		p := entry.Value.Parameters
		if !known {
			if len(p) != 3 {
				continue // nothing to compare against
			}
			hk.name = "系统快捷键"
		}
		if entry.Enabled != nil {
			hk.enabled = *entry.Enabled
		}
		if len(p) == 3 {
			hk.keyCode, hk.flags = p[1], p[2]
		}
		result[id] = hk
	}
	return result
}

func (hk symbolicHotkey) accelerator() (string, bool) {
	key, ok := macKeyNames[hk.keyCode]
	if !ok {
		return "", false
	}
	a := Accelerator{Key: key}
	if hk.flags&nsControl != 0 {
		a.Mods |= Ctrl
	}
	if hk.flags&nsOption != 0 {
		a.Mods |= Alt
	}
	if hk.flags&nsShift != 0 {
		a.Mods |= Shift
	}
	if hk.flags&nsCommand != 0 {
		a.Mods |= Super
	}
	// Shortcuts a hotkey could never equal, such as bare letters, are skipped.
	canonical, err := Canonical(a.String())
	return canonical, err == nil
}

type symbolicEntry struct {
	Enabled *bool `plist:"enabled"`
	Value   struct {
		Parameters []int `plist:"parameters"`
	} `plist:"value"`
}

// readSymbolicHotkeys reads the user's changes through `defaults`, which goes
// through cfprefsd and so sees changes not yet flushed to the plist file.
// Failures leave only the defaults in effect.
func readSymbolicHotkeys() map[int]symbolicEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/defaults", "export", "com.apple.symbolichotkeys", "-").Output()
	if err != nil {
		return nil
	}
	var doc struct {
		Hotkeys map[string]symbolicEntry `plist:"AppleSymbolicHotKeys"`
	}
	if err := plist.NewDecoder(bytes.NewReader(out)).Decode(&doc); err != nil {
		return nil
	}
	result := make(map[int]symbolicEntry, len(doc.Hotkeys))
	for k, v := range doc.Hotkeys {
		if id, err := strconv.Atoi(k); err == nil {
			result[id] = v
		}
	}
	return result
}
