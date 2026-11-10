package platform

import (
	"cmp"
	"errors"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"
)

// File Explorer's own icon positions: read before the layer hides its
// icons, and put back when the layer goes, so the desktop is left the way
// it was found.

var (
	procClientToScreen = user32.NewProc("ClientToScreen")

	clsidShellWindows   = windows.GUID{Data1: 0x9BA05972, Data2: 0xF6A8, Data3: 0x11CF, Data4: [8]byte{0xA4, 0x42, 0x00, 0xA0, 0xC9, 0x0A, 0x8F, 0x39}}
	iidIShellWindows    = windows.GUID{Data1: 0x85CB6900, Data2: 0x4D95, Data3: 0x11CF, Data4: [8]byte{0x96, 0x0C, 0x00, 0x80, 0xC7, 0xF4, 0xEE, 0x85}}
	iidIServiceProvider = windows.GUID{Data1: 0x6D5140C1, Data2: 0x7436, Data3: 0x11CE, Data4: [8]byte{0x80, 0x34, 0x00, 0xAA, 0x00, 0x60, 0x09, 0xFA}}
	sidSTopLevelBrowser = windows.GUID{Data1: 0x4C96BE40, Data2: 0x915C, Data3: 0x11CF, Data4: [8]byte{0x99, 0xD3, 0x00, 0xAA, 0x00, 0x4A, 0xE8, 0x37}}
	iidIShellBrowser    = windows.GUID{Data1: 0x000214E2, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIFolderView      = windows.GUID{Data1: 0xCDE725B0, Data2: 0xCCC9, Data3: 0x4519, Data4: [8]byte{0x91, 0x7E, 0x32, 0x5D, 0x72, 0xFA, 0xB4, 0xCE}}
	iidIEnumIDList      = windows.GUID{Data1: 0x000214F2, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	slotSWFindWindowSW           = 15 // IShellWindows
	slotSPQueryService           = 3  // IServiceProvider
	slotSBQueryActiveShellView   = 15 // IShellBrowser
	slotFVItems                  = 8  // IFolderView
	slotFVGetItemPosition        = 11
	slotFVGetSpacing             = 12
	slotFVGetAutoArrange         = 14
	slotFVSelectAndPositionItems = 16

	clsctxAll        = 0x17
	vtI4             = 3
	swcDesktop       = 8
	swfoNeedDispatch = 1
	svgioAllView     = 2
	svsiPositionItem = 0x80
)

// variant is a VARIANT holding a 32-bit integer or nothing.
type variant struct {
	vt  uint16
	_   [3]uint16
	val [2]uint64
}

// iconLayout is where File Explorer had the desktop's icons.
type iconLayout struct {
	at      map[string]point // by parsing name, in the view's coordinates
	spacing point            // the size of a grid cell
	origin  point            // the view's top left corner on the screen
}

// explorerView returns File Explorer's view of the desktop.
func explorerView() (comObject, error) {
	var shellWindows comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellWindows)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIShellWindows)), uintptr(unsafe.Pointer(&shellWindows))); !succeeded(hr) {
		return nil, errors.New("无法连接资源管理器")
	}
	defer release(shellWindows)
	loc := variant{vt: vtI4} // CSIDL_DESKTOP
	var root variant
	var hwnd int32
	var disp comObject
	if hr := call(shellWindows, slotSWFindWindowSW, uintptr(unsafe.Pointer(&loc)), uintptr(unsafe.Pointer(&root)), swcDesktop,
		uintptr(unsafe.Pointer(&hwnd)), swfoNeedDispatch, uintptr(unsafe.Pointer(&disp))); hr != sOK || disp == nil {
		return nil, errors.New("找不到资源管理器的桌面")
	}
	defer release(disp)
	var provider comObject
	if !succeeded(call(disp, slotQueryInterface, uintptr(unsafe.Pointer(&iidIServiceProvider)), uintptr(unsafe.Pointer(&provider)))) {
		return nil, errors.New("找不到资源管理器的桌面")
	}
	defer release(provider)
	var browser comObject
	if !succeeded(call(provider, slotSPQueryService, uintptr(unsafe.Pointer(&sidSTopLevelBrowser)), uintptr(unsafe.Pointer(&iidIShellBrowser)), uintptr(unsafe.Pointer(&browser)))) {
		return nil, errors.New("找不到资源管理器的桌面")
	}
	defer release(browser)
	var view comObject
	if !succeeded(call(browser, slotSBQueryActiveShellView, uintptr(unsafe.Pointer(&view)))) || view == nil {
		return nil, errors.New("找不到资源管理器的桌面")
	}
	defer release(view)
	var folderView comObject
	if !succeeded(call(view, slotQueryInterface, uintptr(unsafe.Pointer(&iidIFolderView)), uintptr(unsafe.Pointer(&folderView)))) {
		return nil, errors.New("找不到资源管理器的桌面")
	}
	return folderView, nil
}

// viewItems calls f with each item of the view, by parsing name; the child
// ID list is freed after f returns.
func viewItems(fv comObject, f func(key string, child uintptr)) error {
	var enum comObject
	if !succeeded(call(fv, slotFVItems, svgioAllView, uintptr(unsafe.Pointer(&iidIEnumIDList)), uintptr(unsafe.Pointer(&enum)))) || enum == nil {
		return errors.New("无法列出桌面图标")
	}
	defer release(enum)
	root, err := desktopFolder()
	if err != nil {
		return err
	}
	defer release(root)
	for {
		var child uintptr
		var fetched uint32
		if hr := call(enum, slotEnumNext, 1, uintptr(unsafe.Pointer(&child)), uintptr(unsafe.Pointer(&fetched))); hr != sOK || fetched == 0 {
			return nil
		}
		if key := displayName(root, child, shgdnForParsing); key != "" {
			f(key, child)
		}
		procCoTaskMemFree.Call(child)
	}
}

// readIconLayout reads where File Explorer has the icons. It returns nil
// when Explorer arranges them itself, as there is nothing to keep then.
func readIconLayout(folderView uintptr) (*iconLayout, error) {
	fv, err := explorerView()
	if err != nil {
		return nil, err
	}
	defer release(fv)
	if call(fv, slotFVGetAutoArrange) == sOK {
		return nil, nil
	}
	l := &iconLayout{at: map[string]point{}}
	call(fv, slotFVGetSpacing, uintptr(unsafe.Pointer(&l.spacing)))
	procClientToScreen.Call(folderView, uintptr(unsafe.Pointer(&l.origin)))
	err = viewItems(fv, func(key string, child uintptr) {
		var p point
		if succeeded(call(fv, slotFVGetItemPosition, child, uintptr(unsafe.Pointer(&p)))) {
			l.at[key] = p
		}
	})
	return l, err
}

// restore puts File Explorer's icons back where they were.
func (l *iconLayout) restore() error {
	fv, err := explorerView()
	if err != nil {
		return err
	}
	defer release(fv)
	if call(fv, slotFVGetAutoArrange) == sOK {
		return nil
	}
	var children []uintptr
	var points []point
	err = viewItems(fv, func(key string, child uintptr) {
		if p, ok := l.at[key]; ok {
			if c, _, _ := procILClone.Call(child); c != 0 {
				children = append(children, c)
				points = append(points, p)
			}
		}
	})
	defer func() {
		for _, c := range children {
			procILFree.Call(c)
		}
	}()
	if err != nil || len(children) == 0 {
		return err
	}
	if hr := call(fv, slotFVSelectAndPositionItems, uintptr(len(children)), uintptr(unsafe.Pointer(&children[0])),
		uintptr(unsafe.Pointer(&points[0])), svsiPositionItem); !succeeded(hr) {
		return errors.New("无法还原桌面图标的位置")
	}
	return nil
}

// ExplorerIcon is where File Explorer had an icon before the layer took
// the desktop over.
type ExplorerIcon struct {
	Key string
	// Cell is the icon's grid cell, in screen pixels.
	Cell Box
}

// ExplorerIcons returns where File Explorer had the desktop's icons, in its
// column order, or nothing when it arranged them itself.
func (d *Desktop) ExplorerIcons() []ExplorerIcon {
	l := d.explorer
	if l == nil {
		return nil
	}
	out := make([]ExplorerIcon, 0, len(l.at))
	for k, p := range l.at {
		out = append(out, ExplorerIcon{Key: k, Cell: Box{
			X: float64(l.origin.X + p.X), Y: float64(l.origin.Y + p.Y),
			Width: float64(l.spacing.X), Height: float64(l.spacing.Y),
		}})
	}
	slices.SortFunc(out, func(a, b ExplorerIcon) int {
		return cmp.Or(cmp.Compare(a.Cell.X, b.Cell.X), cmp.Compare(a.Cell.Y, b.Cell.Y))
	})
	return out
}
