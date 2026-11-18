package platform

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RunInTerminal runs a command in a new console window that stays open
// afterwards, so its output stays readable.
//
// It calls CreateProcess itself: os/exec hands a child the null device for
// any standard handle it is not given, and cmd /k, having run the command,
// read its next one from there, met the end, and closed the window. Given
// no handles, cmd uses the new console's.
func RunInTerminal(command string) error {
	cmd, err := windows.UTF16PtrFromString("cmd.exe /k " + command)
	if err != nil {
		return err
	}
	var dir *uint16
	if home, err := os.UserHomeDir(); err == nil {
		if dir, err = windows.UTF16PtrFromString(home); err != nil {
			return err
		}
	}
	si := windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, cmd, nil, nil, false, windows.CREATE_NEW_CONSOLE, nil, dir, &si, &pi); err != nil {
		return err
	}
	_ = windows.CloseHandle(pi.Thread)
	_ = windows.CloseHandle(pi.Process)
	return nil
}
