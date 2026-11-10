package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/shirumesu/arubox/internal/fileindex"
	"github.com/shirumesu/arubox/internal/logging"
	"github.com/shirumesu/arubox/internal/platform"
)

// runHelper runs the command lines the executable has besides the program:
// the guard that gives the desktop back to File Explorer, the file index
// service, and installing or removing it as administrator. It reports
// whether it ran one, and exits on failure.
func runHelper() bool {
	if platform.RunDesktopGuard(os.Args) {
		return true
	}
	handled, err := fileindex.Main(os.Args)
	if handled && err != nil {
		if dir, derr := os.UserCacheDir(); derr == nil {
			if out, closeLog, lerr := logging.Open(filepath.Join(dir, "arubox", "logs")); lerr == nil {
				logging.New(out, slog.LevelInfo).Error("file index helper failed", "args", os.Args[1:], "error", err)
				closeLog()
			}
		}
		os.Exit(1)
	}
	return handled
}
