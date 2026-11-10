package screenshot

import (
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
		img, ok := h.module.currentImage()
		if !ok {
			http.Error(w, "no capture", http.StatusNotFound)
			return
		}
		img.ServeBMP(w)
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
