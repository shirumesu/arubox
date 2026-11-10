package platform

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The shell side of the desktop layer: the desktop's items as File Explorer
// sees them, and its menus, verbs, renaming and dragging, so everything
// behaves as on the real desktop.

var (
	procSHGetDesktopFolder         = shell32.NewProc("SHGetDesktopFolder")
	procSHParseDisplayName         = shell32.NewProc("SHParseDisplayName")
	procSHBindToParent             = shell32.NewProc("SHBindToParent")
	procSHGetNameFromIDList        = shell32.NewProc("SHGetNameFromIDList")
	procSHChangeNotifyRegister     = shell32.NewProc("SHChangeNotifyRegister")
	procSHChangeNotifyDeregister   = shell32.NewProc("SHChangeNotifyDeregister")
	procSHChangeNotificationLock   = shell32.NewProc("SHChangeNotification_Lock")
	procSHChangeNotificationUnlock = shell32.NewProc("SHChangeNotification_Unlock")
	procSHDoDragDrop               = shell32.NewProc("SHDoDragDrop")
	procILCombine                  = shell32.NewProc("ILCombine")
	procILFindLastID               = shell32.NewProc("ILFindLastID")
	procILFree                     = shell32.NewProc("ILFree")
	shlwapi                        = windows.NewLazySystemDLL("shlwapi.dll")
	procStrRetToStrW               = shlwapi.NewProc("StrRetToStrW")
	procCoTaskMemFree              = ole32.NewProc("CoTaskMemFree")
	procILClone                    = shell32.NewProc("ILClone")
	procShellExecuteExW            = shell32.NewProc("ShellExecuteExW")
	procSHGetSetSettings           = shell32.NewProc("SHGetSetSettings")
)

var (
	iidIShellFolder      = windows.GUID{Data1: 0x000214E6, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIContextMenu      = windows.GUID{Data1: 0x000214e4, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIContextMenu2     = windows.GUID{Data1: 0x000214f4, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIContextMenu3     = windows.GUID{Data1: 0xbcfce0a0, Data2: 0xec17, Data3: 0x11d0, Data4: [8]byte{0x8d, 0x10, 0x00, 0xa0, 0xc9, 0x0f, 0x27, 0x19}}
	iidIDataObject       = windows.GUID{Data1: 0x0000010e, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDropTarget       = windows.GUID{Data1: 0x00000122, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIUnknown          = windows.GUID{Data1: 0x00000000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	clsidDragDropHelper  = windows.GUID{Data1: 0x4657278A, Data2: 0x411B, Data3: 0x11D2, Data4: [8]byte{0x83, 0x9A, 0x00, 0xC0, 0x4F, 0xD9, 0x18, 0xD0}}
	iidIDropTargetHelper = windows.GUID{Data1: 0x4657278B, Data2: 0x411B, Data3: 0x11D2, Data4: [8]byte{0x83, 0x9A, 0x00, 0xC0, 0x4F, 0xD9, 0x18, 0xD0}}
	clsidFileOpenDialog  = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4dde, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog   = windows.GUID{Data1: 0xd57c7288, Data2: 0xd4ad, Data3: 0x4768, Data4: [8]byte{0xbe, 0x02, 0x9d, 0x96, 0x95, 0x32, 0xd9, 0x60}}
	recycleBinCLSID      = "{645FF040-5081-101B-9F08-00AA002F954E}"
)

const (
	slotSFEnumObjects      = 4 // IShellFolder
	slotSFBindToObject     = 5
	slotSFCreateViewObject = 8
	slotSFGetAttributesOf  = 9
	slotSFGetUIObjectOf    = 10
	slotSFGetDisplayNameOf = 11
	slotSFSetNameOf        = 12
	slotEnumNext           = 3 // IEnumIDList
	slotAddRef             = 1

	slotCMQueryContextMenu = 3 // IContextMenu
	slotCMInvokeCommand    = 4
	slotCMGetCommandString = 5
	slotCM2HandleMenuMsg   = 6
	slotCM3HandleMenuMsg2  = 7

	slotDTDragEnter = 3 // IDropTarget
	slotDTDragOver  = 4
	slotDTDragLeave = 5
	slotDTDrop      = 6

	slotDTHDragEnter = 3 // IDropTargetHelper
	slotDTHDragLeave = 4
	slotDTHDragOver  = 5
	slotDTHDrop      = 6

	slotFDShow       = 3 // IFileOpenDialog
	slotFDSetOptions = 9
	slotFDGetOptions = 10
	slotFDGetResult  = 20
	slotSIGetDisplay = 5 // IShellItem

	shcontfFolders       = 0x20
	shcontfNonFolders    = 0x40
	shcontfIncludeHidden = 0x80
	shgdnNormal          = 0x0
	shgdnInFolder        = 0x1
	shgdnForEditing      = 0x1000
	shgdnForParsing      = 0x8000
	sigdnDesktopAbsParse = 0x80028000
	sigdnFileSysPath     = 0x80058000
	sfgaoFolder          = 0x20000000
	sfgaoStream          = 0x00400000
	sfgaoFileSystem      = 0x40000000

	cmfNormal       = 0x0
	cmfExtended     = 0x100
	cmfCanRename    = 0x10
	gcsVerbW        = 0x4
	cmicUnicode     = 0x4000
	cmicPtInvoke    = 0x20000000
	cmicShiftDown   = 0x10000000
	cmicControlDown = 0x40000000
	menuFirstShell  = 0x1000
	menuLastShell   = 0x7FFF

	seeMaskIDList  = 0x4
	fosPickFolders = 0x20
	fosForceFS     = 0x40

	shcnrfInterrupt = 0x0001
	shcnrfShell     = 0x0002
	shcnrfNewDeliv  = 0x8000
	shcneRenameItem = 0x00000001
	shcneRenameDir  = 0x00020000
	shcneAll        = 0x7FFFFFFF

	shChangeNotifyEntrySize = 12

	dropEffectNone = 0
	dropEffectCopy = 1
	dropEffectMove = 2
	dropEffectLink = 4
	sOK            = 0
	eNoInterface   = 0x80004002
	ssfShowAllObj  = 0x1
)

// ShellItem is an item of the desktop or of a folder.
type ShellItem struct {
	// Key is the parsing name: a path for files, ::{CLSID} for items such
	// as the Recycle Bin.
	Key  string
	Name string
	// Folder is a folder one can open into; an archive is not one.
	Folder   bool
	Ext      string // the extension, lowercased, of files
	Modified time.Time
	id       *idList
}

// idList owns an absolute item ID list and frees it when dropped.
type idList struct{ p uintptr }

func newIDList(p uintptr) *idList {
	l := &idList{p: p}
	runtime.SetFinalizer(l, func(l *idList) { procILFree.Call(l.p) })
	return l
}

func desktopFolder() (comObject, error) {
	var f comObject
	if hr, _, _ := procSHGetDesktopFolder.Call(uintptr(unsafe.Pointer(&f))); !succeeded(hr) {
		return nil, errors.New("无法打开桌面")
	}
	return f, nil
}

// DesktopItems lists what the desktop shows: the files of the user's and
// the public desktop folders, and the system items the user chose to show,
// such as the Recycle Bin.
func DesktopItems() ([]ShellItem, error) {
	root, err := desktopFolder()
	if err != nil {
		return nil, err
	}
	defer release(root)
	shown := shownSystemIcons()
	return enumerate(root, 0, func(it ShellItem) bool {
		// Namespace extensions, such as the Recycle Bin, or the user's
		// folder and Music, whose parsing names are file system paths, are
		// ::{CLSID} by their names in the desktop.
		if clsid, ok := strings.CutPrefix(displayName(root, it.id.p, shgdnInFolder|shgdnForParsing), "::"); ok {
			return shown(clsid)
		}
		return true
	})
}

// FolderItems lists a folder's items.
func FolderItems(path string) ([]ShellItem, error) {
	abs, err := parseName(path)
	if err != nil {
		return nil, err
	}
	defer procILFree.Call(abs)
	root, err := desktopFolder()
	if err != nil {
		return nil, err
	}
	defer release(root)
	var folder comObject
	if !succeeded(call(root, slotSFBindToObject, abs, 0, uintptr(unsafe.Pointer(&iidIShellFolder)), uintptr(unsafe.Pointer(&folder)))) {
		return nil, errors.New("无法打开文件夹 " + path)
	}
	defer release(folder)
	return enumerate(folder, abs, func(ShellItem) bool { return true })
}

func parseName(path string) (uintptr, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var abs uintptr
	if hr, _, _ := procSHParseDisplayName.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&abs)), 0, 0); !succeeded(hr) {
		return 0, errors.New("找不到 " + path)
	}
	return abs, nil
}

// enumerate lists a folder's items; parent is the folder's absolute ID
// list, or 0 for the desktop itself.
func enumerate(folder comObject, parent uintptr, keep func(ShellItem) bool) ([]ShellItem, error) {
	flags := uintptr(shcontfFolders | shcontfNonFolders)
	if showHidden() {
		flags |= shcontfIncludeHidden
	}
	var enum comObject
	if hr := call(folder, slotSFEnumObjects, 0, flags, uintptr(unsafe.Pointer(&enum))); !succeeded(hr) || enum == nil {
		return nil, errors.New("无法列出项目")
	}
	defer release(enum)
	var items []ShellItem
	for {
		var child uintptr
		var fetched uint32
		if hr := call(enum, slotEnumNext, 1, uintptr(unsafe.Pointer(&child)), uintptr(unsafe.Pointer(&fetched))); hr != sOK || fetched == 0 {
			break
		}
		item, ok := makeItem(folder, parent, child)
		procILFree.Call(child)
		if ok && keep(item) {
			items = append(items, item)
		}
	}
	return items, nil
}

func makeItem(folder comObject, parent, child uintptr) (ShellItem, bool) {
	key := displayName(folder, child, shgdnForParsing)
	name := displayName(folder, child, shgdnInFolder)
	if key == "" {
		return ShellItem{}, false
	}
	attrs := uint32(sfgaoFolder | sfgaoStream | sfgaoFileSystem)
	call(folder, slotSFGetAttributesOf, 1, uintptr(unsafe.Pointer(&child)), uintptr(unsafe.Pointer(&attrs)))
	abs, _, _ := procILCombine.Call(parent, child)
	item := ShellItem{Key: key, Name: name, Folder: attrs&sfgaoFolder != 0 && attrs&sfgaoStream == 0, id: newIDList(abs)}
	if attrs&sfgaoFileSystem != 0 {
		if !item.Folder {
			item.Ext = strings.ToLower(filepath.Ext(key))
		}
		if info, err := os.Stat(key); err == nil {
			item.Modified = info.ModTime()
		}
	}
	return item, true
}

func displayName(folder comObject, child uintptr, flags uintptr) string {
	var ret [272]byte // STRRET
	if !succeeded(call(folder, slotSFGetDisplayNameOf, child, flags, uintptr(unsafe.Pointer(&ret[0])))) {
		return ""
	}
	var s *uint16
	if hr, _, _ := procStrRetToStrW.Call(uintptr(unsafe.Pointer(&ret[0])), child, uintptr(unsafe.Pointer(&s))); !succeeded(hr) || s == nil {
		return ""
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(s)))
	return windows.UTF16PtrToString(s)
}

// shownSystemIcons reports which system items the desktop shows, as the
// Desktop icon settings keep them. Only the Recycle Bin shows by default.
func shownSystemIcons() func(clsid string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\HideDesktopIcons\NewStartPanel`, registry.QUERY_VALUE)
	return func(clsid string) bool {
		if err == nil {
			if v, _, verr := k.GetIntegerValue(clsid); verr == nil {
				return v == 0
			}
		}
		return strings.EqualFold(clsid, recycleBinCLSID)
	}
}

// showHidden reports whether File Explorer shows hidden files.
func showHidden() bool {
	var state uint32 // SHELLSTATE; fShowAllObjects is its first bit
	procSHGetSetSettings.Call(uintptr(unsafe.Pointer(&state)), ssfShowAllObj, 0)
	return state&1 != 0
}

// EditName returns the name an item is renamed from, as File Explorer
// offers it: without the extension when extensions are hidden.
func EditName(item ShellItem) string {
	folder, child, err := bindParent(item)
	if err != nil {
		return item.Name
	}
	defer release(folder)
	if n := displayName(folder, child, shgdnInFolder|shgdnForEditing); n != "" {
		return n
	}
	return item.Name
}

// RenameItem renames an item and returns its new parsing name.
func RenameItem(item ShellItem, name string) (string, error) {
	folder, child, err := bindParent(item)
	if err != nil {
		return "", err
	}
	defer release(folder)
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var renamed uintptr
	if hr := call(folder, slotSFSetNameOf, 0, child, uintptr(unsafe.Pointer(n)), shgdnInFolder|shgdnForEditing, uintptr(unsafe.Pointer(&renamed))); !succeeded(hr) {
		return "", errors.New("重命名失败")
	}
	defer procILFree.Call(renamed)
	return displayName(folder, renamed, shgdnForParsing), nil
}

// bindParent returns an item's folder and its ID list within it, which
// belongs to the item.
func bindParent(item ShellItem) (comObject, uintptr, error) {
	if item.id == nil {
		return nil, 0, errors.New("项目已不存在")
	}
	var folder comObject
	var child uintptr
	if hr, _, _ := procSHBindToParent.Call(item.id.p, uintptr(unsafe.Pointer(&iidIShellFolder)), uintptr(unsafe.Pointer(&folder)), uintptr(unsafe.Pointer(&child))); !succeeded(hr) {
		return nil, 0, errors.New("无法打开项目所在的文件夹")
	}
	runtime.KeepAlive(item.id)
	return folder, child, nil
}

// uiObject asks the folder of items, which share one, for an interface on
// them, such as their context menu or data for dragging.
func uiObject(items []ShellItem, iid *windows.GUID) (comObject, error) {
	if len(items) == 0 {
		return nil, errors.New("没有项目")
	}
	folder, _, err := bindParent(items[0])
	if err != nil {
		return nil, err
	}
	defer release(folder)
	children := make([]uintptr, len(items))
	for i, it := range items {
		if it.id == nil {
			return nil, errors.New("项目已不存在")
		}
		children[i], _, _ = procILFindLastID.Call(it.id.p)
	}
	var obj comObject
	if !succeeded(call(folder, slotSFGetUIObjectOf, 0, uintptr(len(children)), uintptr(unsafe.Pointer(&children[0])), uintptr(unsafe.Pointer(iid)), 0, uintptr(unsafe.Pointer(&obj)))) {
		return nil, errors.New("无法操作这些项目")
	}
	runtime.KeepAlive(items)
	return obj, nil
}

// folderObject asks a folder, by path, for an interface on its background:
// its context menu or its drop target.
func folderObject(path string, iid *windows.GUID) (comObject, error) {
	abs, err := parseName(path)
	if err != nil {
		return nil, err
	}
	defer procILFree.Call(abs)
	root, err := desktopFolder()
	if err != nil {
		return nil, err
	}
	defer release(root)
	var folder comObject
	if !succeeded(call(root, slotSFBindToObject, abs, 0, uintptr(unsafe.Pointer(&iidIShellFolder)), uintptr(unsafe.Pointer(&folder)))) {
		return nil, errors.New("无法打开文件夹 " + path)
	}
	defer release(folder)
	var obj comObject
	if !succeeded(call(folder, slotSFCreateViewObject, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj)))) {
		return nil, errors.New("无法操作文件夹 " + path)
	}
	return obj, nil
}

// DesktopFolder is the path of the user's desktop folder, where what is
// dropped or pasted on the desktop goes.
func DesktopFolder() string {
	p, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if err != nil {
		return ""
	}
	return p
}

// MenuRename is what ItemMenu returns when the user chose to rename, which
// the caller does itself since there is no File Explorer view to do it.
const MenuRename = -1

// ItemMenu shows File Explorer's context menu for items of one folder at a
// screen point, below the caller's own entries, and carries out what the
// user chooses there. It returns the ID of an own entry chosen, MenuRename,
// or 0.
func (d *Desktop) ItemMenu(items []ShellItem, own []MenuItem, x, y float64, extended bool) int {
	cm, err := uiObject(items, &iidIContextMenu)
	if err != nil {
		return 0
	}
	return d.shellMenu(cm, own, x, y, extended, true)
}

// FolderMenu shows the context menu of a folder's background, such as New
// and Paste, below the caller's own entries.
func (d *Desktop) FolderMenu(path string, own []MenuItem, x, y float64) int {
	cm, err := folderObject(path, &iidIContextMenu)
	if err != nil {
		return d.Menu(own, x, y)
	}
	return d.shellMenu(cm, own, x, y, false, false)
}

func (d *Desktop) shellMenu(cm comObject, own []MenuItem, x, y float64, extended, rename bool) int {
	defer release(cm)
	s := d.surfaceAt(x, y)
	if s == nil {
		return 0
	}
	m := buildMenu(own)
	defer procDestroyMenu.Call(m)
	if len(own) > 0 {
		procAppendMenuW.Call(m, mfSeparator, 0, 0)
	}
	flags := uintptr(cmfNormal)
	if rename {
		flags |= cmfCanRename
	}
	if extended {
		flags |= cmfExtended
	}
	n := uintptr(len(own) + 1)
	if len(own) == 0 {
		n = 0
	}
	call(cm, slotCMQueryContextMenu, m, n, menuFirstShell, menuLastShell, flags)
	// Submenus such as Send to and Open with fill themselves through menu
	// messages, which the surface hands to the menu while it shows.
	var cm3 comObject
	if !succeeded(call(cm, slotQueryInterface, uintptr(unsafe.Pointer(&iidIContextMenu3)), uintptr(unsafe.Pointer(&cm3)))) {
		call(cm, slotQueryInterface, uintptr(unsafe.Pointer(&iidIContextMenu2)), uintptr(unsafe.Pointer(&cm3)))
	}
	d.menu = cm3
	chosen := d.track(s, m, x, y)
	d.menu = nil
	if cm3 != nil {
		release(cm3)
	}
	switch {
	case chosen <= 0:
		return 0
	case chosen < menuFirstShell:
		return chosen
	}
	verb := commandVerb(cm, uintptr(chosen-menuFirstShell))
	if verb == "rename" {
		return MenuRename
	}
	invoke(cm, uintptr(chosen-menuFirstShell), "", s.hwnd, x, y)
	return 0
}

func handleMenuMessage(cm comObject, m, w, l uintptr) (uintptr, bool) {
	var result uintptr
	if succeeded(call(cm, slotCM3HandleMenuMsg2, m, w, l, uintptr(unsafe.Pointer(&result)))) {
		return result, true
	}
	if succeeded(call(cm, slotCM2HandleMenuMsg, m, w, l)) {
		return 0, true
	}
	return 0, false
}

func commandVerb(cm comObject, id uintptr) string {
	buf := make([]uint16, 128)
	if !succeeded(call(cm, slotCMGetCommandString, id, gcsVerbW, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))) {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// invoke carries out a menu command, by its offset or, when verb is set,
// by its verb.
func invoke(cm comObject, id uintptr, verb string, hwnd uintptr, x, y float64) {
	var info struct {
		Size        uint32
		Mask        uint32
		Hwnd        uintptr
		Verb        uintptr
		Parameters  uintptr
		Directory   uintptr
		Show        int32
		HotKey      uint32
		Icon        uintptr
		Title       uintptr
		VerbW       uintptr
		ParametersW uintptr
		DirectoryW  uintptr
		TitleW      uintptr
		Invoke      point
	}
	info.Size = uint32(unsafe.Sizeof(info))
	info.Mask = cmicUnicode | cmicPtInvoke
	ctrl, shift, _ := modifiers()
	if ctrl {
		info.Mask |= cmicControlDown
	}
	if shift {
		info.Mask |= cmicShiftDown
	}
	info.Hwnd = hwnd
	info.Show = showNormal
	info.Invoke = point{int32(x), int32(y)}
	info.Verb, info.VerbW = id, id
	var a []byte
	var w *uint16
	if verb != "" {
		a = append([]byte(verb), 0)
		w, _ = windows.UTF16PtrFromString(verb)
		info.Verb, info.VerbW = uintptr(unsafe.Pointer(&a[0])), uintptr(unsafe.Pointer(w))
	}
	call(cm, slotCMInvokeCommand, uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(a)
	runtime.KeepAlive(w)
}

// InvokeVerb carries out a verb on items, such as "delete", "copy", "cut"
// or "properties", as their context menu would.
func (d *Desktop) InvokeVerb(items []ShellItem, verb string, x, y float64) {
	cm, err := uiObject(items, &iidIContextMenu)
	if err != nil {
		return
	}
	defer release(cm)
	m := buildMenu(nil)
	defer procDestroyMenu.Call(m)
	call(cm, slotCMQueryContextMenu, m, 0, menuFirstShell, menuLastShell, cmfNormal)
	var hwnd uintptr
	if s := d.surfaceAt(x, y); s != nil {
		hwnd = s.hwnd
	}
	invoke(cm, 0, verb, hwnd, x, y)
}

// PasteInto pastes what the clipboard holds into a folder.
func (d *Desktop) PasteInto(path string, x, y float64) {
	cm, err := folderObject(path, &iidIContextMenu)
	if err != nil {
		return
	}
	defer release(cm)
	m := buildMenu(nil)
	defer procDestroyMenu.Call(m)
	call(cm, slotCMQueryContextMenu, m, 0, menuFirstShell, menuLastShell, cmfNormal)
	var hwnd uintptr
	if s := d.surfaceAt(x, y); s != nil {
		hwnd = s.hwnd
	}
	invoke(cm, 0, "paste", hwnd, x, y)
}

// OpenItem opens an item with its default verb, as a double-click would.
func OpenItem(item ShellItem) error {
	if item.id == nil {
		return errors.New("项目已不存在")
	}
	info := shellExecuteInfoW{Mask: seeMaskIDList, IDList: item.id.p, Show: showNormal}
	info.Size = uint32(unsafe.Sizeof(info))
	if r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return nil
		}
		return errors.New("无法打开 " + item.Name)
	}
	runtime.KeepAlive(item.id)
	return nil
}

type shellExecuteInfoW struct {
	Size       uint32
	Mask       uint32
	Hwnd       uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	InstApp    uintptr
	IDList     uintptr
	Class      *uint16
	KeyClass   uintptr
	HotKey     uint32
	Icon       uintptr
	Process    uintptr
}

// Drag drags items out with OLE, as File Explorer does, until they are
// dropped; drops on the layer itself come back through the handler as
// internal.
func (d *Desktop) Drag(items []ShellItem) {
	data, err := uiObject(items, &iidIDataObject)
	if err != nil {
		return
	}
	defer release(data)
	var hwnd uintptr
	if len(d.surfaces) > 0 {
		hwnd = d.surfaces[0].hwnd
	}
	var effect uint32
	d.dragging = true
	procSHDoDragDrop.Call(hwnd, uintptr(data), 0, dropEffectCopy|dropEffectMove|dropEffectLink, uintptr(unsafe.Pointer(&effect)))
	d.dragging = false
}

// PickFolder asks the user for a folder, or returns "" when cancelled.
func (d *Desktop) PickFolder() string {
	var dlg comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, 1, uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg))); !succeeded(hr) {
		return ""
	}
	defer release(dlg)
	var opts uint32
	call(dlg, slotFDGetOptions, uintptr(unsafe.Pointer(&opts)))
	call(dlg, slotFDSetOptions, uintptr(opts|fosPickFolders|fosForceFS))
	if !succeeded(call(dlg, slotFDShow, 0)) {
		return ""
	}
	var item comObject
	if !succeeded(call(dlg, slotFDGetResult, uintptr(unsafe.Pointer(&item)))) {
		return ""
	}
	defer release(item)
	var p *uint16
	if !succeeded(call(item, slotSIGetDisplay, sigdnFileSysPath, uintptr(unsafe.Pointer(&p)))) {
		return ""
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return windows.UTF16PtrToString(p)
}

// Watch reports changes of the desktop and of the given folders through
// the handler's ShellChanged.
func (d *Desktop) Watch(folders []string) {
	if d.watch != 0 && strings.Join(folders, "\n") == strings.Join(d.watched, "\n") {
		return
	}
	d.unwatch()
	var lists []uintptr
	paths := append([]string{DesktopFolder()}, folders...)
	if p, err := windows.KnownFolderPath(windows.FOLDERID_PublicDesktop, 0); err == nil {
		paths = append(paths, p)
	}
	for _, p := range paths {
		if abs, err := parseName(p); err == nil {
			lists = append(lists, abs)
		}
	}
	defer func() {
		for _, l := range lists {
			procILFree.Call(l)
		}
	}()
	if len(lists) == 0 {
		return
	}
	// SHChangeNotifyEntry is byte-packed: an ID list and a BOOL that
	// leaves the folders unwatched below, in 12 bytes.
	entries := make([]byte, shChangeNotifyEntrySize*len(lists))
	for i, l := range lists {
		binary.LittleEndian.PutUint64(entries[i*shChangeNotifyEntrySize:], uint64(l))
	}
	id, _, _ := procSHChangeNotifyRegister.Call(d.host, shcnrfInterrupt|shcnrfShell|shcnrfNewDeliv, shcneAll, wmShellNotify,
		uintptr(len(lists)), uintptr(unsafe.Pointer(&entries[0])))
	d.watch, d.watched = uint32(id), folders
}

func (d *Desktop) unwatch() {
	if d.watch != 0 {
		procSHChangeNotifyDeregister.Call(uintptr(d.watch))
		d.watch = 0
	}
}

func (d *Desktop) shellNotified(w, l uintptr) {
	var pidls *[2]uintptr
	var event int32
	lock, _, _ := procSHChangeNotificationLock.Call(w, l, uintptr(unsafe.Pointer(&pidls)), uintptr(unsafe.Pointer(&event)))
	if lock == 0 {
		return
	}
	var renames [][2]string
	if event&(shcneRenameItem|shcneRenameDir) != 0 && pidls != nil && pidls[0] != 0 && pidls[1] != 0 {
		renames = append(renames, [2]string{absoluteName(pidls[0]), absoluteName(pidls[1])})
	}
	procSHChangeNotificationUnlock.Call(lock)
	d.handler.ShellChanged(renames)
}

func absoluteName(pidl uintptr) string {
	var s *uint16
	if hr, _, _ := procSHGetNameFromIDList.Call(pidl, sigdnDesktopAbsParse, uintptr(unsafe.Pointer(&s))); !succeeded(hr) || s == nil {
		return ""
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(s)))
	return windows.UTF16PtrToString(s)
}

// DropKind is what a drag over the desktop would drop onto.
type DropKind int

const (
	// DropNone refuses the drop.
	DropNone DropKind = iota
	// DropHere moves the layer's own items within it, which the handler
	// does in Dropped.
	DropHere
	// DropFolder drops into the folder at Path.
	DropFolder
	// DropItem drops onto the item, such as a folder or a program.
	DropItem
)

// DropTarget is what a drag drops onto.
type DropTarget struct {
	Kind DropKind
	Path string
	Item ShellItem
}

func (t DropTarget) same(o DropTarget) bool {
	return t.Kind == o.Kind && t.Path == o.Path && t.Item.Key == o.Item.Key
}

// dropTarget is the IDropTarget of a surface. It asks the handler what is
// under the pointer and hands the drag to the shell's drop target for it,
// so files dropped in are copied or moved as on the desktop.
type dropTarget struct {
	vtbl    *[7]uintptr
	refs    int32
	d       *Desktop
	s       *surface
	data    comObject
	current DropTarget
	shell   comObject // the shell's drop target for current
	helper  comObject // draws the drag image
}

var dropVtbl = [7]uintptr{
	windows.NewCallback(dropQueryInterface),
	windows.NewCallback(dropAddRef),
	windows.NewCallback(dropRelease),
	windows.NewCallback(dropDragEnter),
	windows.NewCallback(dropDragOver),
	windows.NewCallback(dropDragLeave),
	windows.NewCallback(dropDrop),
}

// dropTargets keeps the targets reachable while COM holds them.
var dropTargets = map[uintptr]*dropTarget{}

func newDropTarget(d *Desktop, s *surface) *dropTarget {
	t := &dropTarget{vtbl: &dropVtbl, refs: 1, d: d, s: s}
	dropTargets[uintptr(unsafe.Pointer(t))] = t
	var helper comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidDragDropHelper)), 0, 1, uintptr(unsafe.Pointer(&iidIDropTargetHelper)), uintptr(unsafe.Pointer(&helper))); succeeded(hr) {
		t.helper = helper
	}
	return t
}

func dropOf(this uintptr) *dropTarget { return dropTargets[this] }

func dropQueryInterface(this uintptr, iid *windows.GUID, out *uintptr) uintptr {
	if *iid == iidIUnknown || *iid == iidIDropTarget {
		*out = this
		dropAddRef(this)
		return sOK
	}
	*out = 0
	return eNoInterface
}

func dropAddRef(this uintptr) uintptr {
	return uintptr(atomic.AddInt32(&dropOf(this).refs, 1))
}

func dropRelease(this uintptr) uintptr {
	t := dropOf(this)
	n := atomic.AddInt32(&t.refs, -1)
	if n == 0 {
		release(t.helper)
		delete(dropTargets, this)
	}
	return uintptr(n)
}

func pointOf(pt uintptr) (float64, float64) {
	return float64(int32(uint32(pt))), float64(int32(uint32(pt >> 32)))
}

func dropDragEnter(this uintptr, data comObject, keys, pt uintptr, effect *uint32) uintptr {
	t := dropOf(this)
	t.data = data
	call(t.data, slotAddRef)
	t.current = DropTarget{}
	if t.helper != nil {
		p := point{int32(uint32(pt)), int32(uint32(pt >> 32))}
		call(t.helper, slotDTHDragEnter, t.s.hwnd, uintptr(data), uintptr(unsafe.Pointer(&p)), uintptr(*effect))
	}
	return t.over(keys, pt, effect)
}

func dropDragOver(this, keys, pt uintptr, effect *uint32) uintptr {
	t := dropOf(this)
	if t.helper != nil {
		p := point{int32(uint32(pt)), int32(uint32(pt >> 32))}
		call(t.helper, slotDTHDragOver, uintptr(unsafe.Pointer(&p)), uintptr(*effect))
	}
	return t.over(keys, pt, effect)
}

// over follows the drag to the target under the pointer.
func (t *dropTarget) over(keys, pt uintptr, effect *uint32) uintptr {
	x, y := pointOf(pt)
	target := t.d.handler.DropAt(x, y, t.d.dragging)
	if !target.same(t.current) {
		t.leaveShell()
		t.current = target
		switch target.Kind {
		case DropFolder:
			t.shell, _ = folderObject(target.Path, &iidIDropTarget)
		case DropItem:
			t.shell, _ = uiObject([]ShellItem{target.Item}, &iidIDropTarget)
		}
		if t.shell != nil {
			e := *effect
			call(t.shell, slotDTDragEnter, uintptr(t.data), keys, pt, uintptr(unsafe.Pointer(&e)))
		}
	}
	switch {
	case target.Kind == DropHere:
		*effect = dropEffectMove
	case t.shell != nil:
		call(t.shell, slotDTDragOver, keys, pt, uintptr(unsafe.Pointer(effect)))
	default:
		*effect = dropEffectNone
	}
	return sOK
}

func (t *dropTarget) leaveShell() {
	if t.shell != nil {
		call(t.shell, slotDTDragLeave)
		release(t.shell)
		t.shell = nil
	}
}

func dropDragLeave(this uintptr) uintptr {
	t := dropOf(this)
	if t.helper != nil {
		call(t.helper, slotDTHDragLeave)
	}
	t.leaveShell()
	t.end()
	return sOK
}

func (t *dropTarget) end() {
	if t.data != nil {
		release(t.data)
		t.data = nil
	}
	t.current = DropTarget{}
}

func dropDrop(this uintptr, data comObject, keys, pt uintptr, effect *uint32) uintptr {
	t := dropOf(this)
	if t.helper != nil {
		p := point{int32(uint32(pt)), int32(uint32(pt >> 32))}
		call(t.helper, slotDTHDrop, uintptr(data), uintptr(unsafe.Pointer(&p)), uintptr(*effect))
	}
	t.over(keys, pt, effect)
	x, y := pointOf(pt)
	target := t.current
	switch {
	case target.Kind == DropHere:
		// The handler moves the items; nothing happens to the files, so
		// the drag source must not delete them.
		*effect = dropEffectNone
	case t.shell != nil:
		call(t.shell, slotDTDrop, uintptr(data), keys, pt, uintptr(unsafe.Pointer(effect)))
		release(t.shell)
		t.shell = nil
	default:
		*effect = dropEffectNone
	}
	t.end()
	if target.Kind != DropNone {
		t.d.handler.Dropped(x, y, target)
	}
	return sOK
}
