package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/shirumesu/arubox/internal/fileindex"
	"github.com/shirumesu/arubox/internal/logging"
	"github.com/shirumesu/arubox/internal/ocr"
	"github.com/shirumesu/arubox/internal/platform"
	"github.com/shirumesu/arubox/internal/update"
)

// runHelper runs the command lines the executable has besides the program:
// the guard that gives the desktop back to File Explorer, the OCR worker,
// the file index service, and installing or removing it as administrator.
// It reports whether it ran one, and exits on failure. A new version
// started by an update first waits for the old one to exit, then runs as
// the program.
func runHelper() bool {
	if platform.RunDesktopGuard(os.Args) || ocr.RunWorker(os.Args) {
		return true
	}
	if err := update.AwaitPrevious(os.Args); err != nil {
		logHelperFailure("waiting for the replaced version to exit failed", err)
		os.Exit(1)
	}
	handled, err := fileindex.Main(os.Args)
	if handled && err != nil {
		logHelperFailure("file index helper failed", err)
		os.Exit(1)
	}
	return handled
}

// logHelperFailure writes to the program's log, as a helper has no window.
func logHelperFailure(message string, err error) {
	if dir, derr := os.UserCacheDir(); derr == nil {
		if out, closeLog, lerr := logging.Open(filepath.Join(dir, "arubox", "logs")); lerr == nil {
			logging.New(out, slog.LevelInfo).Error(message, "args", os.Args[1:], "error", err)
			closeLog()
		}
	}
}
