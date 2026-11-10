package platform

import (
	"os"
	"os/exec"
	"syscall"
)

const createNewConsole = 0x00000010

// RunInTerminal runs a command in a new console window that stays open
// afterwards, so its output stays readable.
func RunInTerminal(command string) error {
	cmd := exec.Command("cmd.exe")
	// cmd parses its own command line; /k keeps the window open.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd.exe /k " + command, CreationFlags: createNewConsole}
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	return cmd.Start()
}
