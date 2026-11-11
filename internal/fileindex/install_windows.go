package fileindex

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Command-line flags of the program's own executable.
const (
	flagService = "--file-index-service"
	flagInstall = "--install-file-index"
	flagRemove  = "--remove-file-index"
)

// Main handles the command lines the index uses: the service manager
// starting the service, and the elevated copy of the program that installs
// or removes it. It reports whether args was one of them.
func Main(args []string) (bool, error) {
	if len(args) < 2 {
		return false, nil
	}
	switch args[1] {
	case flagService:
		if len(args) < 3 {
			return true, errors.New("the index service needs its user")
		}
		return true, runService(args[2])
	case flagInstall:
		if len(args) < 3 {
			return true, errors.New("installing the index service needs its user")
		}
		return true, install(args[2])
	case flagRemove:
		return true, remove()
	}
	return false, nil
}

// installedPath is where the service's executable is copied to: a folder
// only administrators can change, since the service runs as the system and
// whoever could replace its executable could run anything as the system.
func installedPath() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "arubox", "arubox-index.exe"), nil
}

// install copies this executable to the install folder and sets it up as a
// service that starts with Windows, replacing an older one.
func install(sid string) error {
	if _, err := windows.StringToSid(sid); err != nil {
		return fmt.Errorf("bad user %q: %w", sid, err)
	}
	target, err := installedPath()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the service manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(serviceName)
	exists := err == nil
	if exists {
		defer s.Close()
		if err := stopService(s); err != nil {
			return err
		}
	}
	// A stopped service's process lets go of its executable a moment after
	// it reports stopped, and updating the program replaces it every time.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		err = copyExecutable(target)
		if err == nil || !exists || time.Now().After(deadline) {
			break
		}
	}
	if err != nil {
		return err
	}
	config := mgr.Config{
		ServiceType:  windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
		DisplayName:  "arubox 文件索引",
		Description:  "读取文件表并跟随变更，为 arubox 的启动器提供文件搜索。",
	}
	if exists {
		config.BinaryPathName = syscall.EscapeArg(target) + " " + flagService + " " + sid
		if err := s.UpdateConfig(config); err != nil {
			return fmt.Errorf("update the service: %w", err)
		}
	} else {
		s, err = m.CreateService(serviceName, target, config, flagService, sid)
		if err != nil {
			return fmt.Errorf("create the service: %w", err)
		}
		defer s.Close()
	}
	restart := []mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}}
	if err := s.SetRecoveryActions(restart, uint32((24 * time.Hour).Seconds())); err != nil {
		return fmt.Errorf("set the service to restart: %w", err)
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("start the service: %w", err)
	}
	return nil
}

// remove stops and deletes the service and its executable.
func remove() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the service manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(serviceName)
	if err == nil {
		err = stopService(s)
		if err == nil {
			err = s.Delete()
		}
		s.Close()
		if err != nil {
			return err
		}
	}
	target, err := installedPath()
	if err != nil {
		return err
	}
	// The stopped service's process lets go of its executable a moment
	// after it reports stopped.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		err = os.Remove(target)
		if err == nil || errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			break
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete the service's executable: %w", err)
	}
	os.Remove(filepath.Dir(target))
	return nil
}

// stopService stops a running service and waits until it has.
func stopService(s *mgr.Service) error {
	st, err := s.Query()
	if err != nil {
		return fmt.Errorf("query the service: %w", err)
	}
	if st.State == svc.Stopped {
		return nil
	}
	if st.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil {
			return fmt.Errorf("stop the service: %w", err)
		}
	}
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if st, err = s.Query(); err != nil {
			return fmt.Errorf("query the service: %w", err)
		}
		if st.State == svc.Stopped {
			return nil
		}
	}
	return errors.New("the service did not stop")
}

func copyExecutable(target string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	src, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("copy the executable: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("copy the executable: %w", err)
	}
	return dst.Close()
}

// serviceState is what the program can tell about the service without
// administrator rights.
type serviceState int

const (
	serviceMissing serviceState = iota
	serviceStopped
	serviceRunning
)

func queryService() (serviceState, error) {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return serviceMissing, err
	}
	defer func() { _ = windows.CloseServiceHandle(m) }()
	name, _ := windows.UTF16PtrFromString(serviceName)
	h, err := windows.OpenService(m, name, windows.SERVICE_QUERY_STATUS)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return serviceMissing, nil
	}
	if err != nil {
		return serviceMissing, err
	}
	defer func() { _ = windows.CloseServiceHandle(h) }()
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return serviceMissing, err
	}
	if st.CurrentState == windows.SERVICE_RUNNING {
		return serviceRunning, nil
	}
	return serviceStopped, nil
}

var (
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	size          uint32
	mask          uint32
	hwnd          uintptr
	verb          *uint16
	file          *uint16
	parameters    *uint16
	directory     *uint16
	show          int32
	instApp       uintptr
	idList        uintptr
	class         *uint16
	keyClass      uintptr
	hotKey        uint32
	iconOrMonitor uintptr
	process       windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	errCancelled          = windows.Errno(1223)
)

// errDeclined is returned when the user turns down the administrator
// prompt.
var errDeclined = errors.New("已取消管理员授权")

// runElevated runs this executable with args as administrator, which asks
// the user, and waits for it to finish.
func runElevated(args string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return runElevatedFile(exe, args)
}

// runElevatedFile runs exe with args as administrator and waits for it.
func runElevatedFile(exe, args string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(args)
	if err != nil {
		return err
	}
	info := &shellExecuteInfo{mask: seeMaskNoCloseProcess | seeMaskNoAsync, verb: verb, file: file, parameters: params}
	info.size = uint32(unsafe.Sizeof(*info))
	if ok, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(info))); ok == 0 {
		if errors.Is(err, errCancelled) {
			return errDeclined
		}
		return fmt.Errorf("以管理员身份运行失败：%w", err)
	}
	defer closeHandle(info.process)
	if _, err := windows.WaitForSingleObject(info.process, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.process, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("退出码 %d，详情见日志", code)
	}
	return nil
}

// currentUser returns the SID of the user running the program.
func currentUser() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}
