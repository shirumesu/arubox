package screenshot

import (
	"encoding/binary"
	"net/http"
	"strconv"
	"strings"
)

// imageRoute is where the pages load pictures from: /screenshot/capture.bmp
// is the current capture, /screenshot/pin/<id>.png a pinned picture.
const imageRoute = "/screenshot/"

// images serves the pictures to the pages, so they never travel as text
// through the bindings.
type images struct {
	module *Module
}

func (h images) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, imageRoute)
	w.Header().Set("Cache-Control", "no-store")
	if path == "capture.bmp" {
		h.serveCapture(w)
		return
	}
	if id, ok := strings.CutPrefix(path, "pin/"); ok {
		n, err := strconv.Atoi(strings.TrimSuffix(id, ".png"))
		if png, found := h.module.pins.picture(n); err == nil && found {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
	}
	http.NotFound(w, r)
}

// serveCapture writes the capture as an uncompressed bitmap: its BGRA rows
// go out as they are, without spending time on compression.
func (h images) serveCapture(w http.ResponseWriter) {
	img, ok := h.module.currentImage()
	if !ok {
		http.Error(w, "no capture", http.StatusNotFound)
		return
	}
	const fileHeader, infoHeader = 14, 40
	header := make([]byte, fileHeader+infoHeader)
	size := len(img.Pixels)
	copy(header, "BM")
	// Screen sizes are far from overflowing these fields.
	binary.LittleEndian.PutUint32(header[2:], uint32(fileHeader+infoHeader+size)) //nolint:gosec
	binary.LittleEndian.PutUint32(header[10:], fileHeader+infoHeader)
	binary.LittleEndian.PutUint32(header[14:], infoHeader)
	binary.LittleEndian.PutUint32(header[18:], uint32(img.Width)) //nolint:gosec
	// A negative height stores the rows top-down.
	binary.LittleEndian.PutUint32(header[22:], uint32(-int32(img.Height))) //nolint:gosec
	binary.LittleEndian.PutUint16(header[26:], 1)
	binary.LittleEndian.PutUint16(header[28:], 32)
	w.Header().Set("Content-Type", "image/bmp")
	w.Header().Set("Content-Length", strconv.Itoa(len(header)+size))
	_, _ = w.Write(header)
	_, _ = w.Write(img.Pixels)
}
