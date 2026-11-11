// Package ort runs ONNX models with an ONNX Runtime library that is loaded
// from a file at run time, not linked. It calls the library's C API without
// cgo: OrtGetApiBase hands out OrtApi, a table of function pointers, and
// every call goes through a slot of that table. Only what the OCR engine
// needs is here: CPU sessions of one float input and one float output.
//
// What is loaded stays loaded for the life of the process. The OCR engine
// runs it in a process of its own and frees its memory by ending that
// process, which ONNX Runtime and the system allocator do not do as
// thoroughly when they are asked to.
package ort

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Version is the ONNX Runtime release the slots below are taken from. The
// table only ever grows, so they stay valid in later releases too.
const Version = "1.31.1"

// apiVersion is ORT_API_VERSION of that release, asked for from GetApi.
const apiVersion = 31

// Slots of the functions used, counted from the top of struct OrtApi in
// onnxruntime_c_api.h.
const (
	slotGetErrorMessage                = 2
	slotDisableTelemetryEvents         = 6
	slotCreateSession                  = 7
	slotRun                            = 9
	slotCreateSessionOptions           = 10
	slotDisableMemPattern              = 17
	slotDisableCPUMemArena             = 19
	slotSetSessionGraphOptimization    = 23
	slotSessionGetInputName            = 36
	slotSessionGetOutputName           = 37
	slotCreateTensorAsOrtValue         = 48
	slotGetTensorMutableData           = 51
	slotGetDimensionsCount             = 61
	slotGetDimensions                  = 62
	slotGetTensorTypeAndShape          = 65
	slotGetAllocatorWithDefaultOptions = 78
	slotReleaseStatus                  = 93
	slotReleaseValue                   = 96
	slotReleaseTensorTypeAndShapeInfo  = 99
	slotReleaseSessionOptions          = 100
	slotCreateEnvWithGlobalThreadPools = 119
	slotDisablePerSessionThreads       = 120
	slotCreateThreadingOptions         = 121
	slotReleaseThreadingOptions        = 122
	slotSetGlobalIntraOpNumThreads     = 147
	slotSetGlobalInterOpNumThreads     = 148
	slotSetGlobalSpinControl           = 149
)

const (
	logLevelError     = 3  // ORT_LOGGING_LEVEL_ERROR
	optimizeAll       = 99 // ORT_ENABLE_ALL
	elementTypeFloat  = 1  // ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT
	maxDimensionCount = 8
)

// Runtime is a loaded library with one environment, whose thread pool all
// its sessions share.
type Runtime struct {
	api       unsafe.Pointer // const OrtApi*
	env       uintptr
	allocator uintptr // the default CPU allocator, owned by the library
}

// Load loads the library at path and creates an environment whose shared
// thread pool runs an operator on up to threads threads. The threads do not
// spin waiting for work, so an idle runtime takes no CPU time. Telemetry,
// which ONNX Runtime sends to Microsoft by default, is turned off; on macOS
// that also takes ORT_DISABLE_TELEMETRY in the process's environment before
// Load, as only that keeps it from starting.
func Load(path string, threads int) (*Runtime, error) {
	getAPIBase, err := openLibrary(path)
	if err != nil {
		return nil, err
	}
	base, _, _ := purego.SyscallN(getAPIBase)
	if base == 0 {
		return nil, errors.New("ONNX Runtime 库无效")
	}
	// OrtApiBase is { GetApi, GetVersionString }.
	getAPI := *(*uintptr)(cPointer(base))
	api, _, _ := purego.SyscallN(getAPI, apiVersion)
	if api == 0 {
		return nil, fmt.Errorf("ONNX Runtime 版本过旧，需要 %s", Version)
	}
	r := &Runtime{api: cPointer(api)}
	if err := r.createEnv(threads); err != nil {
		return nil, err
	}
	if err := r.call(slotDisableTelemetryEvents, r.env); err != nil {
		return nil, err
	}
	if err := r.call(slotGetAllocatorWithDefaultOptions, uintptr(unsafe.Pointer(&r.allocator))); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Runtime) createEnv(threads int) error {
	var options uintptr
	if err := r.call(slotCreateThreadingOptions, uintptr(unsafe.Pointer(&options))); err != nil {
		return err
	}
	defer r.release(slotReleaseThreadingOptions, options)
	if err := r.call(slotSetGlobalIntraOpNumThreads, options, uintptr(threads)); err != nil {
		return err
	}
	if err := r.call(slotSetGlobalInterOpNumThreads, options, 1); err != nil {
		return err
	}
	if err := r.call(slotSetGlobalSpinControl, options, 0); err != nil {
		return err
	}
	logID := []byte("arubox\x00")
	return r.call(slotCreateEnvWithGlobalThreadPools, logLevelError, uintptr(unsafe.Pointer(&logID[0])), options, uintptr(unsafe.Pointer(&r.env)))
}

// fn returns the function in a slot of the table.
func (r *Runtime) fn(slot int) uintptr {
	return *(*uintptr)(unsafe.Add(r.api, slot*int(unsafe.Sizeof(uintptr(0)))))
}

// call calls a function that returns an OrtStatus, and turns a status into
// an error. Arguments are often pointers to out-parameters converted to
// uintptr; the directive keeps what they point to on the heap for the call,
// as for com_windows.go's call.
//
//go:uintptrescapes
func (r *Runtime) call(slot int, args ...uintptr) error {
	status, _, _ := purego.SyscallN(r.fn(slot), args...)
	if status == 0 {
		return nil
	}
	msg, _, _ := purego.SyscallN(r.fn(slotGetErrorMessage), status)
	err := errors.New("ONNX Runtime：" + goString(msg))
	r.release(slotReleaseStatus, status)
	return err
}

// release calls a Release function, which takes the object and returns
// nothing.
func (r *Runtime) release(slot int, object uintptr) {
	if object != 0 {
		purego.SyscallN(r.fn(slot), object)
	}
}

// Session is a loaded model.
type Session struct {
	rt     *Runtime
	handle uintptr
	// input and output are the names of the first input and output, in
	// the library's memory.
	input, output uintptr
}

// NewSession loads the model at path. Its graph is fully optimised, and it
// allocates what each run needs and frees it afterwards rather than keeping
// an arena: inputs change size from run to run, and an arena would hold on
// to the largest.
func (r *Runtime) NewSession(path string) (*Session, error) {
	var options uintptr
	if err := r.call(slotCreateSessionOptions, uintptr(unsafe.Pointer(&options))); err != nil {
		return nil, err
	}
	defer r.release(slotReleaseSessionOptions, options)
	for _, slot := range []int{slotDisablePerSessionThreads, slotDisableCPUMemArena, slotDisableMemPattern} {
		if err := r.call(slot, options); err != nil {
			return nil, err
		}
	}
	if err := r.call(slotSetSessionGraphOptimization, options, optimizeAll); err != nil {
		return nil, err
	}
	p, err := pathArg(path)
	if err != nil {
		return nil, err
	}
	s := &Session{rt: r}
	if err := r.call(slotCreateSession, r.env, uintptr(p), options, uintptr(unsafe.Pointer(&s.handle))); err != nil {
		return nil, err
	}
	if err := r.call(slotSessionGetInputName, s.handle, 0, r.allocator, uintptr(unsafe.Pointer(&s.input))); err != nil {
		return nil, err
	}
	if err := r.call(slotSessionGetOutputName, s.handle, 0, r.allocator, uintptr(unsafe.Pointer(&s.output))); err != nil {
		return nil, err
	}
	return s, nil
}

// Run runs the model on in and returns its first output, which the caller
// releases.
func (s *Session) Run(in *Value) (*Value, error) {
	inputs := []uintptr{in.handle}
	inputNames := []uintptr{s.input}
	outputNames := []uintptr{s.output}
	outputs := []uintptr{0}
	err := s.rt.call(slotRun, s.handle, 0,
		uintptr(unsafe.Pointer(&inputNames[0])), uintptr(unsafe.Pointer(&inputs[0])), 1,
		uintptr(unsafe.Pointer(&outputNames[0])), 1, uintptr(unsafe.Pointer(&outputs[0])))
	if err != nil {
		return nil, err
	}
	out, err := s.rt.wrap(outputs[0])
	if err != nil {
		s.rt.release(slotReleaseValue, outputs[0])
		return nil, err
	}
	return out, nil
}

// Value is a float tensor in the library's memory.
type Value struct {
	rt     *Runtime
	handle uintptr
	data   []float32
	shape  []int64
}

// NewTensor allocates a float tensor of the given shape for the caller to
// fill through Floats.
func (r *Runtime) NewTensor(shape ...int64) (*Value, error) {
	var handle uintptr
	if err := r.call(slotCreateTensorAsOrtValue, r.allocator, uintptr(unsafe.Pointer(&shape[0])), uintptr(len(shape)), elementTypeFloat, uintptr(unsafe.Pointer(&handle))); err != nil {
		return nil, err
	}
	v, err := r.wrap(handle)
	if err != nil {
		r.release(slotReleaseValue, handle)
		return nil, err
	}
	return v, nil
}

// wrap reads the shape and the data of a tensor the library made.
func (r *Runtime) wrap(handle uintptr) (*Value, error) {
	var info uintptr
	if err := r.call(slotGetTensorTypeAndShape, handle, uintptr(unsafe.Pointer(&info))); err != nil {
		return nil, err
	}
	defer r.release(slotReleaseTensorTypeAndShapeInfo, info)
	var count uintptr
	if err := r.call(slotGetDimensionsCount, info, uintptr(unsafe.Pointer(&count))); err != nil {
		return nil, err
	}
	if count == 0 || count > maxDimensionCount {
		return nil, fmt.Errorf("ONNX Runtime：张量有 %d 维", count)
	}
	shape := make([]int64, count)
	if err := r.call(slotGetDimensions, info, uintptr(unsafe.Pointer(&shape[0])), count); err != nil {
		return nil, err
	}
	n := int64(1)
	for _, d := range shape {
		n *= d
	}
	var data unsafe.Pointer
	if err := r.call(slotGetTensorMutableData, handle, uintptr(unsafe.Pointer(&data))); err != nil {
		return nil, err
	}
	v := &Value{rt: r, handle: handle, shape: shape}
	if n > 0 {
		v.data = unsafe.Slice((*float32)(data), n)
	}
	return v, nil
}

// Floats is the tensor's data, valid until Release.
func (v *Value) Floats() []float32 { return v.data }

// Shape is the tensor's size in each dimension.
func (v *Value) Shape() []int64 { return v.shape }

// Release frees the tensor.
func (v *Value) Release() {
	v.rt.release(slotReleaseValue, v.handle)
	v.data = nil
}

// cPointer turns an address the library returned into a pointer. What it
// points to is the library's memory, which the garbage collector neither
// moves nor frees.
func cPointer(address uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&address))
}

// goString copies a NUL-terminated string from the library's memory.
func goString(address uintptr) string {
	if address == 0 {
		return ""
	}
	p := cPointer(address)
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}
