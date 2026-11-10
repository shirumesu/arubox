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

func main() {
	if runHelper() {
		return
	}
	assets, err := fs.Sub(frontend, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	if err := core.Run(assets); err != nil {
		log.Fatal(err)
	}
}
