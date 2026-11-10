package desktop

import "errors"

// view does not exist on macOS, whose desktop the module does not support;
// the registry never enables it there.
type view struct{}

func startView(*Module, stored) (*view, error) {
	return nil, errors.New("桌面整理只支持 Windows")
}

func (*view) stop() {}

func (*view) setSettings(Settings) {}
