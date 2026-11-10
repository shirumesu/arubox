package platform

import (
	"os"
	"os/exec"
)

// RunInTerminal runs a shell command in a new Terminal window, so its output
// stays readable. Terminal runs .command files without asking for the
// Automation permission that scripting it would need; the file deletes
// itself as it starts.
func RunInTerminal(command string) error {
	f, err := os.CreateTemp("", "arubox-*.command")
	if err != nil {
		return err
	}
	script := "#!/bin/zsh -l\nrm -f -- \"$0\"\ncd ~\n" + command + "\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		return err
	}
	return exec.Command("open", "-a", "Terminal", f.Name()).Run()
}
