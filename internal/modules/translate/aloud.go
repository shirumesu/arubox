package translate

import (
	"context"
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/shirumesu/arubox/internal/ui"
)

// readAloud reads text in lang with the voice the settings name, "system"
// or "edge", on its own goroutine. done runs on the UI thread when the
// reading ends, with the error that ended it, or nil when it finished or
// was stopped. The returned function stops it.
func (m *Module) readAloud(text, lang, voice string, done func(error)) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		var err error
		if voice == voiceEdge {
			var mp3 []byte
			if mp3, err = m.speak(ctx, text, lang); err == nil {
				err = playMP3(ctx, mp3)
			}
		} else {
			err = speakSystem(ctx, text, lang)
		}
		if ctx.Err() != nil {
			err = nil
		}
		cancel()
		if err != nil {
			m.log.Warn("read aloud", "voice", voice, "error", err)
		}
		ui.Post(func() { done(err) })
	}()
	return cancel
}

// comObject is a COM interface pointer.
type comObject unsafe.Pointer

// comCall invokes the method in the given slot of the object's virtual
// table. The directive keeps what pointer arguments point to on the heap
// for the call, so a growing stack cannot move an out-parameter.
//
//go:uintptrescapes
func comCall(o comObject, slot int, args ...uintptr) uintptr {
	vtable := *(*unsafe.Pointer)(o)
	method := *(*uintptr)(unsafe.Add(vtable, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(o)}, args...)...)
	return r
}

func comRelease(o comObject) {
	if o != nil {
		comCall(o, 2)
	}
}

func succeeded(hr uintptr) bool { return int32(hr) >= 0 }

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

const (
	coinitMultithreaded = 0x0
	clsctxInprocServer  = 0x1
)

// comCreate makes an in-process COM object.
func comCreate(clsid, iid *windows.GUID) (comObject, error) {
	var o comObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&o)))
	if !succeeded(hr) || o == nil {
		return nil, errors.New("COM 对象创建失败")
	}
	return o, nil
}
