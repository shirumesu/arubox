package ocr

import (
	"math"
	"slices"

	"github.com/shirumesu/arubox/internal/platform"
)

// Detection follows the inference.yml shipped with PP-OCRv6_small_det and
// PaddleOCR's general OCR pipeline: the picture keeps its size, rounded to
// multiples of 32, unless a side is under 64 or over 4000 pixels; pixels are
// normalized with the ImageNet mean and deviation, in the order the model
// reads them, blue first.
const (
	detMinSide = 64
	detMaxSide = 4000
	// detMaxPixels also scales down a larger picture. Detection takes about
	// 150 MB a million pixels it looks at, so a whole Retina screen would
	// take over a gigabyte. At this size a full 1080p screen of 12-pixel
	// text loses one short line of 129, and a Retina one of 14-point text
	// none; the lines found are read from the picture at full size.
	detMaxPixels  = 600_000
	detMultiple   = 32
	detThreshold  = 0.2  // a pixel whose text probability is above this is text
	boxThreshold  = 0.45 // a region whose mean probability is below this is dropped
	unclipRatio   = 1.4
	maxCandidates = 3000
	minBoxSide    = 3
)

var detNormalization = normalization{
	mean: [3]float32{0.485, 0.456, 0.406},
	std:  [3]float32{0.229, 0.224, 0.225},
}

// detect finds the boxes of text lines in img, in its pixels, in reading
// order.
//
// The model draws a map of how likely each pixel is part of text, with each
// line shrunk inside its box. Each connected region of likely pixels is a
// line; its box is grown back by the distance the model was trained to
// shrink by. PaddleOCR fits rotated rectangles to the regions; text on a
// screen is level, so the region's bounding box is the same rectangle, and
// is found without OpenCV.
func (p *paddle) detect(img platform.Image) ([]rect, error) {
	width, height := detSize(img.Width, img.Height)
	in, err := p.rt.NewTensor(1, 3, int64(height), int64(width))
	if err != nil {
		return nil, err
	}
	defer in.Release()
	plane := width * height
	resample(img, rect{0, 0, img.Width, img.Height}, width, height, detNormalization, in.Floats(), width, plane)
	out, err := p.det.Run(in)
	if err != nil {
		return nil, err
	}
	defer out.Release()
	shape := out.Shape()
	if len(shape) != 4 || shape[2] != int64(height) || shape[3] != int64(width) {
		return nil, errUnexpectedOutput
	}
	boxes := regions(out.Floats()[:plane], width, height)
	sx := float64(img.Width) / float64(width)
	sy := float64(img.Height) / float64(height)
	result := make([]rect, 0, len(boxes))
	for _, b := range boxes {
		r := rect{
			X0: clip(int(math.Round(b.x0*sx)), img.Width-1),
			Y0: clip(int(math.Round(b.y0*sy)), img.Height-1),
			X1: clip(int(math.Round(b.x1*sx)), img.Width),
			Y1: clip(int(math.Round(b.y1*sy)), img.Height),
		}
		if r.width() <= minBoxSide || r.height() <= minBoxSide {
			continue
		}
		result = append(result, r)
	}
	readingOrder(result)
	return result, nil
}

// detSize is the size the model looks at a picture in.
func detSize(width, height int) (int, int) {
	ratio := 1.0
	if short := min(width, height); short < detMinSide {
		ratio = float64(detMinSide) / float64(short)
	}
	w, h := float64(width)*ratio, float64(height)*ratio
	if long := max(w, h); long > detMaxSide {
		w, h = w*detMaxSide/long, h*detMaxSide/long
	}
	if w*h > detMaxPixels {
		s := math.Sqrt(detMaxPixels / (w * h))
		w, h = w*s, h*s
	}
	round := func(v float64) int {
		return max(int(math.Round(v/detMultiple))*detMultiple, detMultiple)
	}
	return round(w), round(h)
}

// box is a rectangle in the probability map, between pixel centres.
type box struct {
	x0, y0, x1, y1 float64
}

// regions finds the text lines in a probability map of width by height.
func regions(prob []float32, width, height int) []box {
	label := make([]bool, len(prob))
	var stack []int
	var boxes []box
	for start := range prob {
		if label[start] || prob[start] <= detThreshold {
			continue
		}
		if len(boxes) == maxCandidates {
			break
		}
		// Flood the region, eight pixels around each, keeping its bounds.
		x0, y0, x1, y1 := width, height, -1, -1
		label[start] = true
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := i%width, i/width
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= width || ny >= height {
						continue
					}
					j := ny*width + nx
					if !label[j] && prob[j] > detThreshold {
						label[j] = true
						stack = append(stack, j)
					}
				}
			}
		}
		w, h := float64(x1-x0), float64(y1-y0)
		if min(w, h) < minBoxSide || meanProb(prob, width, x0, y0, x1, y1) < boxThreshold {
			continue
		}
		// Grow the box by the distance PaddleOCR's unclip offsets the
		// polygon by: its area times the ratio over its perimeter.
		d := w * h * unclipRatio / (2 * (w + h))
		if min(w, h)+2*d < minBoxSide+2 {
			continue
		}
		boxes = append(boxes, box{float64(x0) - d, float64(y0) - d, float64(x1) + d, float64(y1) + d})
	}
	return boxes
}

// meanProb is the mean probability inside the bounds, edges included.
func meanProb(prob []float32, width, x0, y0, x1, y1 int) float64 {
	var sum float64
	for y := y0; y <= y1; y++ {
		for _, v := range prob[y*width+x0 : y*width+x1+1] {
			sum += float64(v)
		}
	}
	return sum / float64((x1-x0+1)*(y1-y0+1))
}

// readingOrder sorts boxes into lines from top to bottom, and the boxes of
// a line from left to right. A box is on the line of the one above it in
// the order when its middle is level with that box.
func readingOrder(boxes []rect) {
	slices.SortStableFunc(boxes, func(a, b rect) int { return a.Y0 - b.Y0 })
	for start := 0; start < len(boxes); {
		first := boxes[start]
		end := start + 1
		for end < len(boxes) {
			mid := (boxes[end].Y0 + boxes[end].Y1) / 2
			if mid > first.Y1 {
				break
			}
			end++
		}
		slices.SortStableFunc(boxes[start:end], func(a, b rect) int { return a.X0 - b.X0 })
		start = end
	}
}

// clip limits v to 0 up to limit.
func clip(v, limit int) int { return max(0, min(v, limit)) }
