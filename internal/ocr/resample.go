package ocr

import (
	"math"

	"github.com/shirumesu/arubox/internal/platform"
)

// rect is a rectangle of pixels from X0, Y0 up to but not including X1, Y1.
type rect struct {
	X0, Y0, X1, Y1 int
}

func (r rect) width() int  { return r.X1 - r.X0 }
func (r rect) height() int { return r.Y1 - r.Y0 }

// normalization maps a channel value v from 0 to 255 to (v/255 - mean) /
// std, per channel in the order blue, green, red.
type normalization struct {
	mean, std [3]float32
}

// resample scales the part src of img to width by height pixels with
// bilinear filtering, as OpenCV's INTER_LINEAR does, which the models were
// trained with, and writes it normalized into dst as planar blue, green and
// red rows of stride floats each. dst holds three planes of planeSize.
func resample(img platform.Image, src rect, width, height int, n normalization, dst []float32, stride, planeSize int) {
	xs := samples(src.X0, src.width(), width)
	ys := samples(src.Y0, src.height(), height)
	var scale, offset [3]float32
	for c := range 3 {
		scale[c] = 1 / (255 * n.std[c])
		offset[c] = -n.mean[c] / n.std[c]
	}
	for y, sy := range ys {
		row0 := img.Pixels[sy.i0*img.Width*4:]
		row1 := img.Pixels[sy.i1*img.Width*4:]
		wy1 := sy.w
		wy0 := 1 - wy1
		for x, sx := range xs {
			wx1 := sx.w
			wx0 := 1 - wx1
			a, b := sx.i0*4, sx.i1*4
			at := y*stride + x
			for c := range 3 {
				v := wy0*(wx0*float32(row0[a+c])+wx1*float32(row0[b+c])) +
					wy1*(wx0*float32(row1[a+c])+wx1*float32(row1[b+c]))
				dst[c*planeSize+at] = v*scale[c] + offset[c]
			}
		}
	}
}

// sample is where an output pixel takes its value from along one axis: a
// mix of source pixels i0 and i1, weighing i1 by w.
type sample struct {
	i0, i1 int
	w      float32
}

// samples maps size output pixels onto the source pixels from start, of
// which there are n, aligning pixel centres.
func samples(start, n, size int) []sample {
	out := make([]sample, size)
	step := float64(n) / float64(size)
	for i := range out {
		at := (float64(i)+0.5)*step - 0.5
		at = math.Max(at, 0)
		i0 := int(at)
		i0 = min(i0, n-1)
		i1 := min(i0+1, n-1)
		out[i] = sample{i0: start + i0, i1: start + i1, w: float32(at - float64(i0))}
	}
	return out
}
