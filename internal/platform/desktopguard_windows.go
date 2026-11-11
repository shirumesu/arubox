package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"golang.org/x/sys/windows"
)

// While the layer has the desktop, File Explorer's icons are hidden. If the
// program ends without giving the desktop back (a crash, or being ended
// from Task Manager), a guard puts them back: a copy of the program that
// File Explorer starts, so it is no part of the program's process tree, and
// that waits for the program to end. A program that ended before the guard
// could open it is put back the same way.

// flagDesktopGuard is the command line of the guard: the flag and the
// program's process ID.
const flagDesktopGuard = "--desktop-guard"

// guardEventName names the event the program sets when it gives the desktop
// back itself.
func guardEventName(pid int) string { return fmt.Sprintf(`Local\arubox-desktop-%d`, pid) }

// guardLayoutPath is where the program leaves File Explorer's icon
// positions for the guard.
func guardLayoutPath(pid int) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("arubox-desktop-%d.json", pid))
}

// startGuard starts the guard, once File Explorer's positions are known
// and before its icons are hidden.
func (d *Desktop) startGuard() error {
	pid := os.Getpid()
	name, _ := windows.UTF16PtrFromString(guardEventName(pid))
	event, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		return err
	}
	if d.explorer != nil {
		if err := d.explorer.save(guardLayoutPath(pid)); err != nil {
			_ = windows.CloseHandle(event)
			return err
		}
	}
	exe, err := os.Executable()
	if err == nil {
		err = explorerExecute(exe, flagDesktopGuard+" "+strconv.Itoa(pid))
	}
	if err != nil {
		_ = windows.CloseHandle(event)
		os.Remove(guardLayoutPath(pid))
		return err
	}
	d.guard = event
	return nil
}

// stopGuard tells the guard the desktop was given back.
func (d *Desktop) stopGuard() {
	if d.guard == 0 {
		return
	}
	os.Remove(guardLayoutPath(os.Getpid()))
	_ = windows.SetEvent(d.guard)
	_ = windows.CloseHandle(d.guard)
	d.guard = 0
}

// RunDesktopGuard runs the guard when args is its command line, and
// reports whether it was.
func RunDesktopGuard(args []string) bool {
	if len(args) < 3 || args[1] != flagDesktopGuard {
		return false
	}
	pid, err := strconv.Atoi(args[2])
	if err != nil {
		return true
	}
	guardDesktop(pid)
	return true
}

// guardDesktop waits for the program to end and then gives the desktop
// back, unless the program did so itself.
func guardDesktop(pid int) {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// The program is gone already: it ended before the guard got going.
		restoreDesktop(pid)
		return
	}
	defer func() { _ = windows.CloseHandle(process) }()
	name, _ := windows.UTF16PtrFromString(guardEventName(pid))
	event, err := windows.OpenEvent(windows.SYNCHRONIZE, false, name)
	if err != nil {
		// The event lives as long as the program holds it, so it is missing
		// when the program ended, or when it closed the event itself after
		// giving the desktop back.
		if r, err := windows.WaitForSingleObject(process, 0); err == nil && r == windows.WAIT_OBJECT_0 {
			restoreDesktop(pid)
		}
		return
	}
	defer func() { _ = windows.CloseHandle(event) }()
	// The event comes first: when both are set, the program gave the
	// desktop back before it ended.
	r, err := windows.WaitForMultipleObjects([]windows.Handle{event, process}, false, windows.INFINITE)
	if err != nil || r != windows.WAIT_OBJECT_0+1 {
		return
	}
	restoreDesktop(pid)
}

// restoreDesktop shows File Explorer's icons and puts them where the
// program saved them, then removes the saved positions. After a normal
// detach there is nothing left to do.
func restoreDesktop(pid int) {
	runtime.LockOSThread()
	if hr, _, _ := procOleInitialize.Call(0); !succeeded(hr) {
		return
	}
	defer procOleUninitialize.Call()
	if _, folderView := findDesktop(); folderView != 0 {
		procShowWindow.Call(folderView, swShow)
	}
	path := guardLayoutPath(pid)
	if l, err := loadIconLayout(path); err == nil {
		_ = l.restore()
	}
	os.Remove(path)
}
