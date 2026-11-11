package update

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The update package is a zip of the app bundle, made with ditto. It is
// unpacked beside the installed bundle, so that putting it in place is a
// rename on the same volume. The bundle keeps its path, and with it its
// signing identity, so the Accessibility and Screen Recording permissions
// carry over to the new version.

// bundleName is the bundle inside the update package.
const bundleName = "arubox.app"

func platformKey() string { return "macos-" + archName() }

func unavailable() string {
	if !production {
		return "开发版本不检查更新"
	}
	bundle, err := bundlePath()
	if err != nil {
		return err.Error()
	}
	// Gatekeeper runs a quarantined app opened from where it was
	// downloaded from a random read-only copy.
	if strings.Contains(bundle, "/AppTranslocation/") {
		return "请先把 arubox 拖到“应用程序”文件夹再打开，才能自动更新"
	}
	return ""
}

// needsElevation reports whether installing asks for administrator
// rights; on macOS it never does.
func needsElevation() (bool, error) { return false, nil }

// bundlePath is the app bundle the program runs from.
func bundlePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	// arubox.app/Contents/MacOS/arubox
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(bundle) != ".app" {
		return "", fmt.Errorf("程序不在 app 包中运行（%s），无法自动更新", exe)
	}
	return bundle, nil
}

// workDir is where an update is unpacked, and where the replaced bundle
// waits until the new version starts and removes it.
func workDir(bundle string) string {
	return filepath.Join(filepath.Dir(bundle), "."+strings.TrimSuffix(filepath.Base(bundle), ".app")+"-update")
}

// put downloads the package, unpacks it and swaps it in for the running
// bundle, which keeps working from its new place until the program quits.
// The returned function opens the new version once this process has
// exited.
func put(ctx context.Context, client *http.Client, file packageFile, progress func(int64), installing func()) (func() error, error) {
	bundle, err := bundlePath()
	if err != nil {
		return nil, err
	}
	dir := workDir(bundle)
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("无法清理 %s：%w", dir, err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("无法写入 arubox 所在的文件夹 %s：%w", filepath.Dir(bundle), err)
	}
	archive := filepath.Join(dir, "update.zip")
	if err := download(ctx, client, file, archive, progress); err != nil {
		return nil, err
	}
	installing()
	unpacked := filepath.Join(dir, "new")
	// ditto made the package, and keeps the bundle as it was signed.
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", "-x", "-k", archive, unpacked).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("更新包解压失败：%w：%s", err, strings.TrimSpace(string(out)))
	}
	staged := filepath.Join(unpacked, bundleName)
	if _, err := os.Stat(filepath.Join(staged, "Contents", "MacOS", "arubox")); err != nil {
		return nil, fmt.Errorf("更新包中没有 %s：%w", bundleName, err)
	}
	previous := filepath.Join(dir, "previous.app")
	if err := os.Rename(bundle, previous); err != nil {
		return nil, fmt.Errorf("无法移开正在运行的 %s：%w", bundle, err)
	}
	if err := os.Rename(staged, bundle); err != nil {
		if rerr := os.Rename(previous, bundle); rerr != nil {
			return nil, fmt.Errorf("无法放入新版本（%w），也无法放回原来的版本（%w）；原来的版本在 %s", err, rerr, previous)
		}
		return nil, fmt.Errorf("无法放入新版本：%w", err)
	}
	return func() error { return relaunch(bundle) }, nil
}

// relaunch opens bundle once this process has exited. Opened earlier,
// macOS would only bring the running copy forward.
func relaunch(bundle string) error {
	cmd := exec.Command("/bin/sh", "-c",
		`while kill -0 "$1" 2>/dev/null; do sleep 0.2; done; exec /usr/bin/open "$2"`,
		"arubox-relaunch", strconv.Itoa(os.Getpid()), bundle)
	// Its own session, so it outlives the program.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// cleanUp removes the replaced bundle and the unpacked package an update
// left beside the bundle.
func cleanUp() error {
	bundle, err := bundlePath()
	if err != nil {
		return err
	}
	dir := workDir(bundle)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("无法删除上次更新留下的 %s：%w", dir, err)
	}
	return nil
}
