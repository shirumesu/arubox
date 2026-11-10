// Package design reads the design tokens of frontend/src/index.css, which
// define colour, radius and type once for the web pages and for what is
// drawn natively.
package design

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Color is a non-linear sRGB colour with straight alpha, each part 0 to 1.
type Color struct {
	R, G, B, A float64
}

// WithAlpha returns the colour with another alpha.
func (c Color) WithAlpha(a float64) Color {
	c.A = a
	return c
}

// Tokens are the values natively drawn surfaces use.
type Tokens struct {
	// Radius is the corner radius of cards, in pixels at 96 DPI.
	Radius float64
	// Primary is the accent colour of the dark theme, which natively drawn
	// surfaces over the wallpaper use.
	Primary Color
	// Fonts are the font families, in order of preference.
	Fonts []string
}

var (
	radiusRe  = regexp.MustCompile(`--radius:\s*([0-9.]+)rem;`)
	primaryRe = regexp.MustCompile(`--primary:\s*oklch\(([^)]*)\);`)
	fontRe    = regexp.MustCompile(`--font-sans:([^;]*);`)
)

// Parse reads the tokens out of the stylesheet.
func Parse(css []byte) (Tokens, error) {
	s := string(css)
	var t Tokens
	m := radiusRe.FindStringSubmatch(s)
	if m == nil {
		return t, errors.New("design: no --radius")
	}
	rem, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return t, fmt.Errorf("design: --radius: %w", err)
	}
	t.Radius = rem * 16

	dark := strings.Index(s, "prefers-color-scheme: dark)")
	if dark < 0 {
		return t, errors.New("design: no dark theme")
	}
	m = primaryRe.FindStringSubmatch(s[dark:])
	if m == nil {
		return t, errors.New("design: no dark --primary")
	}
	if t.Primary, err = parseOKLCH(m[1]); err != nil {
		return t, err
	}

	m = fontRe.FindStringSubmatch(s)
	if m == nil {
		return t, errors.New("design: no --font-sans")
	}
	for f := range strings.SplitSeq(m[1], ",") {
		f = strings.Trim(strings.TrimSpace(f), `"`)
		if f != "" && f != "sans-serif" && !strings.HasPrefix(f, "-") && f != "BlinkMacSystemFont" {
			t.Fonts = append(t.Fonts, f)
		}
	}
	return t, nil
}

// parseOKLCH reads "L C H" or "L C H / A" as in CSS.
func parseOKLCH(v string) (Color, error) {
	alpha := 1.0
	if body, a, ok := strings.Cut(v, "/"); ok {
		v = body
		pa, err := parsePart(strings.TrimSpace(a))
		if err != nil {
			return Color{}, err
		}
		alpha = pa
	}
	f := strings.Fields(v)
	if len(f) != 3 {
		return Color{}, fmt.Errorf("design: bad oklch(%s)", v)
	}
	var n [3]float64
	for i, p := range f {
		x, err := parsePart(p)
		if err != nil {
			return Color{}, err
		}
		n[i] = x
	}
	return oklch(n[0], n[1], n[2], alpha), nil
}

func parsePart(p string) (float64, error) {
	if pct, ok := strings.CutSuffix(p, "%"); ok {
		x, err := strconv.ParseFloat(pct, 64)
		return x / 100, err
	}
	return strconv.ParseFloat(p, 64)
}

// oklch converts to sRGB, by way of OKLab and linear sRGB.
func oklch(l, c, h, alpha float64) Color {
	rad := h * math.Pi / 180
	a, b := c*math.Cos(rad), c*math.Sin(rad)
	lc := l + 0.3963377774*a + 0.2158037573*b
	mc := l - 0.1055613458*a - 0.0638541728*b
	sc := l - 0.0894841775*a - 1.2914855480*b
	l3, m3, s3 := lc*lc*lc, mc*mc*mc, sc*sc*sc
	return Color{
		R: encode(+4.0767416621*l3 - 3.3077115913*m3 + 0.2309699292*s3),
		G: encode(-1.2684380046*l3 + 2.6097574011*m3 - 0.3413193965*s3),
		B: encode(-0.0041960863*l3 - 0.7034186147*m3 + 1.7076147010*s3),
		A: alpha,
	}
}

// encode applies the sRGB transfer function and clips to the gamut.
func encode(x float64) float64 {
	x = math.Min(math.Max(x, 0), 1)
	if x <= 0.0031308 {
		return 12.92 * x
	}
	return 1.055*math.Pow(x, 1/2.4) - 0.055
}
