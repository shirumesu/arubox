package ocr

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/shirumesu/arubox/internal/platform"
)

// Recognition follows PP-OCRv6_small_rec's inference.yml and PaddleOCR:
// a line is scaled to 48 pixels high keeping its proportions, and padded to
// at least 320 wide. PaddleOCR batches lines; on a CPU, running them one at
// a time is about twice as fast, as no time goes into padding, and the
// output, a probability for every character of the dictionary at every
// eighth pixel across, stays the size of one line's.
const (
	recHeight   = 48
	recMinWidth = 320
	recMaxWidth = 3200
	// minScore drops a line whose characters the model is on average less
	// sure of than this: such lines are mostly icons and other non-text
	// marks read as characters.
	minScore = 0.5
)

var recNormalization = normalization{
	mean: [3]float32{0.5, 0.5, 0.5},
	std:  [3]float32{0.5, 0.5, 0.5},
}

var errUnexpectedOutput = errors.New("PP-OCRv6 模型的输出与预期不符")

// recognize reads the text in each box of img. It returns the lines read,
// in the order of the boxes, leaving out those the model could not read.
func (p *paddle) recognize(img platform.Image, boxes []rect) ([]platform.TextLine, error) {
	lines := make([]platform.TextLine, 0, len(boxes))
	for _, b := range boxes {
		text, score, err := p.recognizeLine(img, b)
		if err != nil {
			return nil, err
		}
		text = strings.TrimSpace(text)
		if text == "" || score < minScore {
			continue
		}
		lines = append(lines, platform.TextLine{
			Text: text,
			Box:  platform.Box{X: float64(b.X0), Y: float64(b.Y0), Width: float64(b.width()), Height: float64(b.height())},
		})
	}
	return lines, nil
}

func (p *paddle) recognizeLine(img platform.Image, b rect) (string, float64, error) {
	// A line longer than the model takes is squeezed to fit, as in
	// PaddleOCR.
	w := min(int(math.Ceil(float64(recHeight)*float64(b.width())/float64(b.height()))), recMaxWidth)
	width := max(w, recMinWidth)
	in, err := p.rt.NewTensor(1, 3, recHeight, int64(width))
	if err != nil {
		return "", 0, err
	}
	defer in.Release()
	data := in.Floats()
	// Padding is zero, the normalized middle grey, as in PaddleOCR.
	clear(data)
	resample(img, b, w, recHeight, recNormalization, data, width, recHeight*width)
	out, err := p.rec.Run(in)
	if err != nil {
		return "", 0, err
	}
	defer out.Release()
	shape := out.Shape()
	if len(shape) != 3 || shape[0] != 1 || shape[2] != int64(len(p.chars)) {
		return "", 0, errUnexpectedOutput
	}
	text, score := p.decode(out.Floats(), int(shape[1]), int(shape[2]))
	return text, score, nil
}

// decode turns the model's per-step probabilities into text by CTC: take
// the likeliest character at each step, merge repeats, drop blanks. The
// score is the mean probability of the characters kept.
func (p *paddle) decode(probs []float32, steps, classes int) (string, float64) {
	var b strings.Builder
	var sum float64
	var n int
	prev := -1
	for t := range steps {
		row := probs[t*classes : (t+1)*classes]
		best := 0
		for c, v := range row {
			if v > row[best] {
				best = c
			}
		}
		if best != prev && best != 0 {
			b.WriteString(p.chars[best])
			sum += float64(row[best])
			n++
		}
		prev = best
	}
	if n == 0 {
		return "", 0
	}
	return b.String(), sum / float64(n)
}

// readDictionary reads the characters the recognition model tells apart
// from its inference.yml, where PostProcess.character_dict lists one per
// line, as PyYAML writes it: plain, or in single quotes with quotes inside
// doubled. The model's classes are a blank, then these, then a space.
func readDictionary(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	chars := []string{""}
	const (
		header = "  character_dict:"
		item   = "  - "
	)
	inDict := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !inDict {
			inDict = line == header
			continue
		}
		v, ok := strings.CutPrefix(line, item)
		if !ok {
			break
		}
		if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		}
		if utf8.RuneCountInString(v) != 1 {
			return nil, fmt.Errorf("识别模型的字典有无法读取的一项：%q", line)
		}
		chars = append(chars, v)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(chars) == 1 {
		return nil, errors.New("识别模型的配置里没有字典")
	}
	return append(chars, " "), nil
}
