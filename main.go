// Command arubox is a personal toolbox: a tray program with a dashboard and
// a set of tool modules that can be enabled one by one.
package main

import (
	"log"

	"github.com/shirumesu/arubox/internal/core"
)

func main() {
	if runHelper() {
		return
	}
	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}
