package platform

import "testing"

func TestParagraphs(t *testing.T) {
	line := func(text string, y float64) TextLine {
		return TextLine{Text: text, Box: Box{X: 10, Y: y, Width: 300, Height: 20}}
	}
	cases := []struct {
		name  string
		lines []TextLine
		want  string
	}{
		{"joins a broken sentence", []TextLine{line("The quick brown", 0), line("fox jumps.", 24)}, "The quick brown fox jumps."},
		{"mends hyphenation", []TextLine{line("transla-", 0), line("tion works", 24)}, "translation works"},
		{"keeps paragraphs apart", []TextLine{line("First.", 0), line("Second.", 60)}, "First.\nSecond."},
		{"joins Chinese without spaces", []TextLine{line("这是一句很长", 0), line("的话。", 24)}, "这是一句很长的话。"},
		{"drops OCR spaces between Chinese", []TextLine{line("中 文 识 别 OCR 结果", 0)}, "中文识别 OCR 结果"},
		{"starts again for a line above", []TextLine{line("Right column", 100), line("Left column", 0)}, "Right column\nLeft column"},
	}
	for _, c := range cases {
		if got := Paragraphs(c.lines); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCrop(t *testing.T) {
	img := Image{Width: 3, Height: 2, Pixels: make([]byte, 3*2*4)}
	for i := range img.Pixels {
		img.Pixels[i] = byte(i / 4) // each pixel holds its index
	}
	got := img.Crop(1, 0, 5, 2)
	if got.Width != 2 || got.Height != 2 || got.Pixels[0] != 1 || got.Pixels[4] != 2 || got.Pixels[8] != 4 {
		t.Errorf("crop = %+v", got)
	}
	if empty := img.Crop(5, 5, 2, 2); empty.Width != 0 {
		t.Errorf("crop outside = %+v", empty)
	}
}
