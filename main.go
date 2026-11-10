// Command arubox is a personal toolbox: a tray program with a dashboard and
// a set of tool modules that can be enabled one by one.
package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/shirumesu/arubox/internal/core"
)

//go:embed all:frontend/dist
var frontend embed.FS

// The design tokens are defined once, in the frontend's stylesheet; what
// is drawn natively reads them from there.
//
//go:embed frontend/src/index.css
var designCSS []byte

func main() {
	if runHelper() {
		return
	}
	assets, err := fs.Sub(frontend, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	if err := core.Run(assets, designCSS); err != nil {
		log.Fatal(err)
	}
}
