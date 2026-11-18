package ui

import (
	"unsafe"
)

const (
	nimAdd         = 0
	nimModify      = 1
	nimDelete      = 2
	nimSetVersion  = 4
	nifMessage     = 0x01
	nifIcon        = 0x02
	nifTip         = 0x04
	nifShowTip     = 0x80
	notifyVersion4 = 4

	ninSelect    = wmUser + 0
	ninKeySelect = ninSelect | 1
)

type notifyIconData struct {
	Size            uint32
	Hwnd            uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        [16]byte
	BalloonIcon     uintptr
}

// MenuItem is one entry of a menu. An item with an empty Label is a
// separator.
type MenuItem struct {
	Label    string
	Disabled bool
	Checked  bool
	Action   func()
}

// Tray is the notification area icon.
type Tray struct {
	icon    uintptr
	tip     string
	OnClick func()
	// Menu returns the items of the right-click menu, built each time it
	// opens.
	Menu func() []MenuItem
}

var tray *Tray

// NewTray adds the tray icon, from PNG or ICO data. Call on the UI thread.
func NewTray(iconData []byte, tip string) *Tray {
	t := &Tray{tip: tip, icon: iconFromData(iconData, 0)}
	tray = t
	t.add()
	return t
}

func (t *Tray) data() notifyIconData {
	d := notifyIconData{Hwnd: mainHwnd, ID: 1, Flags: nifMessage | nifIcon | nifTip | nifShowTip, CallbackMessage: msgTray, Icon: t.icon}
	d.Size = uint32(unsafe.Sizeof(d))
	n := 0
	for _, r := range t.tip {
		if n >= len(d.Tip)-2 {
			break
		}
		if r > 0xFFFF {
			continue
		}
		d.Tip[n] = uint16(r)
		n++
	}
	return d
}

func (t *Tray) add() {
	d := t.data()
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d)))
	d.Version = notifyVersion4
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&d)))
}

// SetTip changes the tooltip.
func (t *Tray) SetTip(tip string) {
	t.tip = tip
	d := t.data()
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

// Remove takes the icon away.
func (t *Tray) Remove() {
	d := t.data()
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
	if tray == t {
		tray = nil
	}
}

func trayRecreate() {
	if tray != nil {
		tray.add()
	}
}

func trayMessage(_, lp uintptr) {
	t := tray
	if t == nil {
		return
	}
	// With NOTIFYICON_VERSION_4 a click also arrives as these two, after the
	// raw button messages; handling both would act twice.
	switch uint32(lp & 0xFFFF) {
	case ninSelect, ninKeySelect:
		if t.OnClick != nil {
			t.OnClick()
		}
	case wmContextMenu:
		if t.Menu != nil {
			var p point
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
			PopupMenu(mainHwnd, int(p.X), int(p.Y), t.Menu())
		}
	}
}

// PopupMenu shows a native menu at a screen point in physical pixels and
// runs the chosen item's action. owner is the window the menu belongs to;
// 0 means the program's hidden main window.
func PopupMenu(owner uintptr, x, y int, items []MenuItem) {
	if owner == 0 {
		owner = mainHwnd
	}
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	for i, it := range items {
		if it.Label == "" {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if it.Disabled {
			flags |= mfGrayed
		}
		if it.Checked {
			flags |= mfChecked
		}
		procAppendMenuW.Call(menu, flags, uintptr(i+1), utf16Ptr(it.Label))
	}
	// The owner must be in front, or the menu does not close when the user
	// clicks elsewhere.
	procSetForegroundWindow.Call(owner)
	cmd, _, _ := procTrackPopupMenuEx.Call(menu, tpmReturnCmd|tpmRightButton, uintptr(x), uintptr(y), owner, 0)
	procPostMessageW.Call(owner, 0, 0, 0)
	if cmd > 0 && int(cmd) <= len(items) {
		if f := items[cmd-1].Action; f != nil {
			f()
		}
	}
}

// iconFromData makes an icon from PNG or ICO bytes; size 0 means the
// system's small icon size.
func iconFromData(data []byte, size int) uintptr {
	if len(data) == 0 {
		return 0
	}
	// CreateIconFromResourceEx takes PNG data as is. For an ICO file, use
	// its first image.
	b := data
	if len(data) > 22 && data[0] == 0 && data[1] == 0 && data[2] == 1 && data[3] == 0 {
		off := int(uint32(data[18]) | uint32(data[19])<<8 | uint32(data[20])<<16 | uint32(data[21])<<24)
		n := int(uint32(data[14]) | uint32(data[15])<<8 | uint32(data[16])<<16 | uint32(data[17])<<24)
		if off+n <= len(data) {
			b = data[off : off+n]
		}
	}
	h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return h
}
