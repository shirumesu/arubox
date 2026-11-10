// Package logging writes the application log to a file in the platform's log
// directory, keeping one rotated copy.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// FileName is the name of the current log file inside the log directory.
const FileName = "arubox.log"

// rotateSize is the size above which the log is rotated at startup.
const rotateSize = 5 << 20

// Open opens dir/arubox.log for appending and returns a writer that also
// copies to stderr when one is attached (development runs). The returned
// function closes the log file.
func Open(dir string) (io.Writer, func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	path := filepath.Join(dir, FileName)
	if info, err := os.Stat(path); err == nil && info.Size() > rotateSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, nil, fmt.Errorf("rotate log: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log: %w", err)
	}
	var out io.Writer = file
	if stderrUsable() {
		out = io.MultiWriter(file, os.Stderr)
	}
	return out, func() { file.Close() }, nil
}

// New returns a logger writing text records at level or above to w.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// stderrUsable reports whether stderr is attached to something. GUI builds on
// Windows have no console, and writing to it would only produce errors.
func stderrUsable() bool {
	_, err := os.Stderr.Stat()
	return err == nil
}
