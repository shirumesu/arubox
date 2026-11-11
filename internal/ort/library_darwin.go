package ort

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// openLibrary loads the library at path and returns OrtGetApiBase.
func openLibrary(path string) (uintptr, error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return 0, fmt.Errorf("加载 ONNX Runtime 失败：%w", err)
	}
	getAPIBase, err := purego.Dlsym(handle, "OrtGetApiBase")
	if err != nil {
		return 0, fmt.Errorf("加载 ONNX Runtime 失败：%w", err)
	}
	return getAPIBase, nil
}

// pathArg is a path as ORTCHAR_T, which is char on macOS.
func pathArg(path string) (unsafe.Pointer, error) {
	b := append([]byte(path), 0)
	return unsafe.Pointer(&b[0]), nil
}
