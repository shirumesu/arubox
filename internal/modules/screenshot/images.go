package screenshot

import (
	"net/http"
	"strconv"
	"strings"
)

// imageRoute is where the pages load pictures from: /screenshot/capture?part=N
// is a strip of the current capture, /screenshot/pin/<id>.png a pinned
// picture.
const imageRoute = "/screenshot/"

// images serves the pictures to the pages, so they never travel as text
// through the bindings.
type images struct {
	module *Module
}

func (h images) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, imageRoute)
	w.Header().Set("Cache-Control", "no-store")
	if path == "capture" {
		img, ok := h.module.currentImage()
		part, err := strconv.Atoi(r.URL.Query().Get("part"))
		if !ok || err != nil {
			http.NotFound(w, r)
			return
		}
		img.ServeStrip(w, r, part)
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
