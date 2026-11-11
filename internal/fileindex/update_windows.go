package fileindex

import "fmt"

// The service runs its own copy of the executable, which an update of the
// program has to replace too: the copy is in a folder only administrators
// can change, and a copy left behind would keep running the old code, or
// stop talking to the program once the protocol changes.

// ServiceInstalled reports whether the index service is installed.
func ServiceInstalled() (bool, error) {
	st, err := queryService()
	if err != nil {
		return false, fmt.Errorf("查询文件索引服务失败：%w", err)
	}
	return st != serviceMissing, nil
}

// UpdateService installs exe, a new version of the program, as the index
// service in place of the installed copy. It asks the user for
// administrator rights and waits until the new service is started.
func UpdateService(exe string) error {
	sid, err := currentUser()
	if err != nil {
		return err
	}
	if err := runElevatedFile(exe, flagInstall+" "+sid); err != nil {
		return fmt.Errorf("更新文件索引服务失败：%w", err)
	}
	return nil
}
