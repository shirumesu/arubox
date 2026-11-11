package ort

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openLibrary loads the DLL at path and returns OrtGetApiBase. The DLLs it
// needs are looked for in its own folder and the system folders only.
func openLibrary(path string) (uintptr, error) {
	handle, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)
	if errors.Is(err, windows.ERROR_MOD_NOT_FOUND) {
		// The file itself is there, so a DLL it needs is not: the Visual
		// C++ runtime, which ONNX Runtime does not carry.
		return 0, errors.New("加载 ONNX Runtime 失败：系统缺少 Microsoft Visual C++ 运行库，请从微软官网安装“Visual C++ Redistributable”后重试")
	}
	if err != nil {
		return 0, fmt.Errorf("加载 ONNX Runtime 失败：%w", err)
	}
	getAPIBase, err := windows.GetProcAddress(handle, "OrtGetApiBase")
	if err != nil {
		return 0, fmt.Errorf("加载 ONNX Runtime 失败：%w", err)
	}
	return getAPIBase, nil
}

// pathArg is a path as ORTCHAR_T, which is wchar_t on Windows.
func pathArg(path string) (unsafe.Pointer, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	return unsafe.Pointer(p), nil
}
