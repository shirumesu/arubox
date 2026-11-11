package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/shirumesu/arubox/internal/fileindex"
)

// The update package is the new arubox.exe. It is downloaded beside the
// running one, which Windows lets be renamed but not overwritten: the
// running file moves aside and the new one takes its name, so autostart
// and shortcuts keep pointing at the program. The desktop guard is the same
// file started again; it keeps running from the moved file until the
// program has given the desktop back. The file index service has its own
// copy under Program Files, which is replaced first, with the new file.

// flagUpdated starts the new version: the flag and the process ID of the
// copy that installed it.
const flagUpdated = "--updated"

func platformKey() string { return "windows-" + archName() }

func unavailable() string {
	if !production {
		return "开发版本不检查更新"
	}
	return ""
}

// needsElevation reports whether installing asks for administrator
// rights: it does when the file index service is installed.
func needsElevation() (bool, error) { return fileindex.ServiceInstalled() }

// files are the paths an update uses: the running executable, the new one
// beside it and the old one once moved aside. The names keep ".exe" so the
// new one can be run as administrator to replace the service's copy.
func files() (exe, staged, old string, err error) {
	exe, err = os.Executable()
	if err != nil {
		return "", "", "", err
	}
	base := strings.TrimSuffix(exe, filepath.Ext(exe))
	return exe, base + ".new.exe", base + ".old.exe", nil
}

// put downloads the new executable, updates the file index service with
// it if the service is installed, and swaps it in for the running one.
// The returned function starts the new version, which waits for this
// process to exit.
func put(ctx context.Context, client *http.Client, file packageFile, progress func(int64), installing func()) (func() error, error) {
	exe, staged, old, err := files()
	if err != nil {
		return nil, err
	}
	if err := download(ctx, client, file, staged, progress); err != nil {
		os.Remove(staged)
		return nil, err
	}
	installing()
	service, err := fileindex.ServiceInstalled()
	if err == nil && service {
		err = fileindex.UpdateService(staged)
	}
	if err != nil {
		os.Remove(staged)
		return nil, err
	}
	// This replaces an old file an earlier update could not remove.
	if err := os.Rename(exe, old); err != nil {
		os.Remove(staged)
		return nil, fmt.Errorf("无法移开正在运行的 %s：%w", exe, err)
	}
	if err := os.Rename(staged, exe); err != nil {
		if rerr := os.Rename(old, exe); rerr != nil {
			return nil, fmt.Errorf("无法放入新版本（%w），也无法放回原来的版本（%w）；原来的版本在 %s", err, rerr, old)
		}
		return nil, fmt.Errorf("无法放入新版本：%w", err)
	}
	return func() error {
		cmd := exec.Command(exe, flagUpdated, strconv.Itoa(os.Getpid()))
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}, nil
}

// AwaitPrevious waits, when args start the new version after an update,
// until the copy that installed it has exited, so that this one does not
// find it still running and hand over to it.
func AwaitPrevious(args []string) error {
	if len(args) < 3 || args[1] != flagUpdated {
		return nil
	}
	pid, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil {
		return fmt.Errorf("bad process ID %q: %w", args[2], err)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		// It has exited already.
		return nil
	}
	if err != nil {
		return fmt.Errorf("open the previous process: %w", err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if _, err := windows.WaitForSingleObject(h, windows.INFINITE); err != nil {
		return fmt.Errorf("wait for the previous process: %w", err)
	}
	return nil
}

// cleanUp removes the old executable an update moved aside.
func cleanUp() error {
	_, staged, old, err := files()
	if err != nil {
		return err
	}
	// A download the program quit during.
	if err := os.Remove(staged); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("无法删除上次更新留下的 %s：%w", staged, err)
	}
	// The desktop guard lets go of the old file a moment after the
	// previous version has exited.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		err = os.Remove(old)
		if err == nil || errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			break
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("无法删除上次更新留下的 %s：%w", old, err)
	}
	return nil
}
