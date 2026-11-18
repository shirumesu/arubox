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
	"strings"
	"sync"
	"syscall"
	"time"
)

// Drawing an app's icon or a desktop item's thumbnail goes through the
// shell's image factory, which loads dozens of DLLs (XAML and the Start
// menu's tile data among them) and keeps their memory for good: about
// 35 MB for app icons, about 40 MB for the thumbnails of 17 desktop items.
// So images are drawn by a helper: a copy of arubox started with
// flagIconHelper, which answers requests on its stdin and stdout and is
// ended once it has been idle for iconHelperIdle.
const (
	flagIconHelper = "--icon-helper"
	iconHelperIdle = 30 * time.Second
)

// Requests are one line each, and each reply is its length as a
// little-endian uint32, then that many bytes:
//
//	icon<TAB>size<TAB>id      a PNG of an app's or file's icon
//	item<TAB>pixels<TAB>name  a shell item's image: width and height as
//	                          little-endian uint32s, then BGRA pixels
//
// An empty reply means there is no image.
const (
	requestIcon = "icon"
	requestItem = "item"
)

// RunIconHelper runs the icon helper when args ask for it, and reports
// whether they did.
func RunIconHelper(args []string) bool {
	if len(args) < 2 || args[1] != flagIconHelper {
		return false
	}
	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return true
		}
		reply := answer(strings.TrimRight(line, "\r\n"))
		_ = binary.Write(out, binary.LittleEndian, uint32(len(reply)))
		_, _ = out.Write(reply)
		if out.Flush() != nil {
			return true
		}
	}
}

// answer draws what a request asks for.
func answer(request string) []byte {
	parts := strings.SplitN(request, "\t", 3)
	if len(parts) != 3 {
		return nil
	}
	size, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil
	}
	switch parts[0] {
	case requestIcon:
		return iconInProcess(parts[2], size)
	case requestItem:
		img := itemImageInProcess(parts[2], size)
		if len(img.Pixels) == 0 {
			return nil
		}
		b := binary.LittleEndian.AppendUint32(nil, uint32(img.Width))
		b = binary.LittleEndian.AppendUint32(b, uint32(img.Height))
		return append(b, img.Pixels...)
	}
	return nil
}

// Icon returns the icon of an app (by ID) or a file, as a PNG of the given
// size in DIPs, at twice the pixels for high-DPI screens, or nil. It asks
// the icon helper and may take a while; call it off the UI thread.
func Icon(id string, size int) []byte {
	return askHelper(requestIcon, size, id)
}

// ItemImage returns what File Explorer shows for a shell item, given by its
// parsing name: a thumbnail where the item has one, its icon otherwise, at
// most pixels wide and high. The pixels are BGRA with premultiplied alpha;
// an empty Image means there is none. It asks the icon helper and may take
// a while; call it off the UI thread.
func ItemImage(parsingName string, pixels int) Image {
	b := askHelper(requestItem, pixels, parsingName)
	if len(b) < 8 {
		return Image{}
	}
	w := int(binary.LittleEndian.Uint32(b))
	h := int(binary.LittleEndian.Uint32(b[4:]))
	if len(b)-8 != w*h*4 {
		return Image{}
	}
	return Image{Pixels: b[8:], Width: w, Height: h}
}

// iconHelper is the running helper, started on the first request.
var iconHelper struct {
	mu    sync.Mutex
	cmd   *exec.Cmd
	in    io.WriteCloser
	out   *bufio.Reader
	timer *time.Timer
	// uses counts requests, so an idle timer that fires after a newer
	// request leaves the helper alone.
	uses uint64
}

// askHelper sends a request to the helper, starting it when needed, and
// returns the reply, or nil.
func askHelper(kind string, size int, target string) []byte {
	if strings.ContainsAny(target, "\r\n") {
		return nil
	}
	h := &iconHelper
	h.mu.Lock()
	defer h.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if h.cmd == nil {
			if err := startIconHelper(); err != nil {
				return nil
			}
		}
		reply, err := exchange(kind, size, target)
		if err == nil {
			h.uses++
			uses := h.uses
			if h.timer != nil {
				h.timer.Stop()
			}
			h.timer = time.AfterFunc(iconHelperIdle, func() {
				h.mu.Lock()
				defer h.mu.Unlock()
				if h.uses == uses {
					stopIconHelper()
				}
			})
			return reply
		}
		// The helper died, perhaps in a shell extension; start another.
		stopIconHelper()
	}
	return nil
}

// startIconHelper starts the helper. iconHelper.mu must be held.
func startIconHelper() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, flagIconHelper)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	iconHelper.cmd, iconHelper.in, iconHelper.out = cmd, in, bufio.NewReader(out)
	return nil
}

// maxReply bounds a reply: a 256-pixel thumbnail is 256 KB.
const maxReply = 16 << 20

// exchange sends one request and reads its reply. iconHelper.mu must be
// held.
func exchange(kind string, size int, target string) ([]byte, error) {
	if _, err := fmt.Fprintf(iconHelper.in, "%s\t%d\t%s\n", kind, size, target); err != nil {
		return nil, err
	}
	var n uint32
	if err := binary.Read(iconHelper.out, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	if n > maxReply {
		return nil, errors.New("icon helper sent " + strconv.Itoa(int(n)) + " bytes")
	}
	reply := make([]byte, n)
	if _, err := io.ReadFull(iconHelper.out, reply); err != nil {
		return nil, err
	}
	return reply, nil
}

// stopIconHelper ends the helper: closing its stdin ends its loop.
// iconHelper.mu must be held.
func stopIconHelper() {
	h := &iconHelper
	if h.cmd == nil {
		return
	}
	_ = h.in.Close()
	cmd := h.cmd
	h.cmd, h.in, h.out = nil, nil, nil
	go func() {
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()
}
