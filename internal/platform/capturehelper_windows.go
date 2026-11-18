package platform

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// The screen is read through DXGI desktop duplication, which is quick and
// gets HDR colours right but needs a Direct3D device: making one loads the
// graphics driver into the process, about 50 MB that stays until the
// process ends. So each capture runs in a short-lived copy of arubox,
// started with flagCapture, which writes the pixels to its stdout and
// exits, taking the driver with it.
const flagCapture = "--capture-screen"

// RunCaptureHelper runs the capture helper when args ask for it, and
// reports whether they did.
func RunCaptureHelper(args []string) bool {
	if len(args) < 6 || args[1] != flagCapture {
		return false
	}
	var n [4]int
	for i := range n {
		v, err := strconv.Atoi(args[2+i])
		if err != nil {
			os.Exit(2)
		}
		n[i] = v
	}
	pixels, err := capturePixels(Rect{X: n[0], Y: n[1], Width: n[2], Height: n[3]})
	out := bufio.NewWriterSize(os.Stdout, 1<<20)
	if err != nil {
		_ = binary.Write(out, binary.LittleEndian, uint32(0))
		_, _ = io.WriteString(out, err.Error())
		_ = out.Flush()
		os.Exit(1)
	}
	_ = binary.Write(out, binary.LittleEndian, uint32(len(pixels)))
	_, _ = out.Write(pixels)
	_ = out.Flush()
	return true
}

// capturePixelsInHelper reads r through the capture helper.
func capturePixelsInHelper(r Rect) ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, flagCapture, strconv.Itoa(r.X), strconv.Itoa(r.Y), strconv.Itoa(r.Width), strconv.Itoa(r.Height))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	in := bufio.NewReaderSize(stdout, 1<<20)
	var size uint32
	readErr := binary.Read(in, binary.LittleEndian, &size)
	var pixels []byte
	var message []byte
	switch {
	case readErr != nil:
	case size == 0:
		message, _ = io.ReadAll(in)
	case int(size) != r.Width*r.Height*4:
		readErr = fmt.Errorf("capture helper sent %d bytes for %dx%d", size, r.Width, r.Height)
	default:
		pixels = make([]byte, size)
		_, readErr = io.ReadFull(in, pixels)
	}
	waitErr := cmd.Wait()
	if len(message) > 0 {
		return nil, errors.New(string(message))
	}
	if readErr != nil {
		return nil, fmt.Errorf("截取屏幕失败：%w", readErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("截取屏幕失败：%w", waitErr)
	}
	return pixels, nil
}
