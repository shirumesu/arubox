package main

import (
	"os"

	"github.com/shirumesu/arubox/internal/ocr"
)

// runHelper runs the command line the executable has besides the program:
// the OCR worker. It reports whether it ran it.
func runHelper() bool { return ocr.RunWorker(os.Args) }
