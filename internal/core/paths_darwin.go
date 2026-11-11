package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// logDirectory is ~/Library/Logs/arubox, where Console.app looks for logs.
func logDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate log directory: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", appName), nil
}

// dataDirectory is ~/Library/Application Support/arubox, beside the
// settings, where downloaded files are kept.
func dataDirectory() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate data directory: %w", err)
	}
	return filepath.Join(dir, appName), nil
}
