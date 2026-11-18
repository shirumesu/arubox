package ui

import (
	"errors"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// What the screenshot module needs beyond the rest of the package: the
// system's Save As dialog.

var (
	comdlg32                 = windows.NewLazySystemDLL("comdlg32.dll")
	procGetSaveFileNameW     = comdlg32.NewProc("GetSaveFileNameW")
	procCommDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")
	procCoUninitialize       = ole32.NewProc("CoUninitialize")
)

const (
	ofnOverwritePrompt = 0x00000002
	ofnHideReadOnly    = 0x00000004
	ofnNoChangeDir     = 0x00000008
	ofnPathMustExist   = 0x00000800
	ofnExplorer        = 0x00080000
)

type openFileName struct {
	StructSize      uint32
	Owner           uintptr
	Instance        uintptr
	Filter          *uint16
	CustomFilter    *uint16
	MaxCustomFilter uint32
	FilterIndex     uint32
	File            *uint16
	MaxFile         uint32
	FileTitle       *uint16
	MaxFileTitle    uint32
	InitialDir      *uint16
	Title           *uint16
	Flags           uint32
	FileOffset      uint16
	FileExtension   uint16
	DefExt          *uint16
	CustData        uintptr
	Hook            uintptr
	TemplateName    *uint16
	Reserved        uintptr
	Reserved2       uint32
	FlagsEx         uint32
}

// FileType is one entry of a file dialog's type list.
type FileType struct {
	Name string
	// Patterns are like "*.png"; the first one's extension is added to a
	// name typed without one.
	Patterns []string
}

// SaveFileOptions configures SaveFileDialog.
type SaveFileOptions struct {
	Title string
	Dir   string
	Name  string
	Types []FileType
}

// SaveFileDialog asks where to save a file with the system's Save As
// dialog and blocks until it closes. It returns "" when cancelled. Call it
// from a goroutine other than the UI thread: it runs its own message loop
// on a thread of its own, so the windows stay responsive meanwhile.
func SaveFileDialog(opts SaveFileOptions) (string, error) {
	if !OnUIThread() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		// The dialog hosts shell views, which need a single-threaded
		// apartment.
		procCoInitializeEx.Call(0, 0x2)
		defer procCoUninitialize.Call()
	}
	var filter strings.Builder
	for _, t := range opts.Types {
		filter.WriteString(t.Name + "\x00" + strings.Join(t.Patterns, ";") + "\x00")
	}
	filter.WriteString("\x00")
	filterU := utf16Slice(filter.String())
	buf := make([]uint16, 32768)
	copy(buf, windowsUTF16(opts.Name))
	ofn := openFileName{
		Filter:      &filterU[0],
		FilterIndex: 1,
		File:        &buf[0],
		MaxFile:     uint32(len(buf)),
		Flags:       ofnOverwritePrompt | ofnHideReadOnly | ofnNoChangeDir | ofnPathMustExist | ofnExplorer,
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	if opts.Dir != "" {
		ofn.InitialDir = utf16(opts.Dir)
	}
	if opts.Title != "" {
		ofn.Title = utf16(opts.Title)
	}
	if len(opts.Types) > 0 && len(opts.Types[0].Patterns) > 0 {
		// With a default extension set, the dialog adds the chosen type's
		// extension to a name typed without one.
		ofn.DefExt = utf16(strings.TrimPrefix(opts.Types[0].Patterns[0], "*."))
	}
	r, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return "", errors.New("无法打开保存对话框")
		}
		return "", nil
	}
	return windows.UTF16ToString(buf), nil
}

// utf16Slice converts s, which may hold NULs, to UTF-16 as is.
func utf16Slice(s string) []uint16 {
	out := make([]uint16, 0, len(s)+1)
	for _, part := range strings.Split(s, "\x00") {
		out = append(out, windowsUTF16(part)...)
		out = append(out, 0)
	}
	return out[:len(out)-1]
}
