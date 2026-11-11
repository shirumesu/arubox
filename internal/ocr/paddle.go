package ocr

import (
	"runtime"

	"github.com/shirumesu/arubox/internal/ort"
	"github.com/shirumesu/arubox/internal/platform"
)

// maxThreads caps the threads an operator runs on. Beyond four the models,
// which are small, gain little, and the other cores stay free.
const maxThreads = 4

// paddle is PP-OCRv6 small, its detection and recognition models loaded in
// ONNX Runtime. It lives in the worker process.
type paddle struct {
	rt       *ort.Runtime
	det, rec *ort.Session
	// chars are the recognition model's classes: a blank, the characters
	// of its dictionary, a space.
	chars []string
}

// loadPaddle loads the library and both models from the installed files.
func loadPaddle(files installedFiles) (*paddle, error) {
	chars, err := readDictionary(files.dictionary)
	if err != nil {
		return nil, err
	}
	rt, err := ort.Load(files.library, min(runtime.NumCPU(), maxThreads))
	if err != nil {
		return nil, err
	}
	p := &paddle{rt: rt, chars: chars}
	if p.det, err = rt.NewSession(files.detection); err != nil {
		return nil, err
	}
	if p.rec, err = rt.NewSession(files.recognition); err != nil {
		return nil, err
	}
	return p, nil
}

// read finds and reads the lines of text in img. Line boxes are in its
// pixels.
func (p *paddle) read(img platform.Image) ([]platform.TextLine, error) {
	boxes, err := p.detect(img)
	if err != nil || len(boxes) == 0 {
		return nil, err
	}
	return p.recognize(img, boxes)
}
