package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// logDirectory is %LocalAppData%\arubox\logs.
func logDirectory() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate log directory: %w", err)
	}
	return filepath.Join(dir, appName, "logs"), nil
}
