// Package design holds the design tokens every surface is drawn with:
// colour, radius and type, for the light and the dark theme.
package design

import "math"

// Color is a non-linear sRGB colour with straight alpha, each part 0 to 1.
type Color struct {
	R, G, B, A float64
}

// WithAlpha returns the colour with another alpha.
func (c Color) WithAlpha(a float64) Color {
	c.A = a
	return c
}

// Mix returns the colour t of the way from c to o.
func (c Color) Mix(o Color, t float64) Color {
	return Color{
		R: c.R + (o.R-c.R)*t,
		G: c.G + (o.G-c.G)*t,
		B: c.B + (o.B-c.B)*t,
		A: c.A + (o.A-c.A)*t,
	}
}

// Palette is one theme's colours.
type Palette struct {
	Background, Foreground         Color
	Card, CardForeground           Color
	Popover, PopoverForeground     Color
	Primary, PrimaryForeground     Color
	Secondary, SecondaryForeground Color
	Muted, MutedForeground         Color
	Accent, AccentForeground       Color
	Destructive                    Color
	Border, Input, Ring            Color
	Sidebar, SidebarForeground     Color
	SidebarAccent                  Color
	SidebarAccentForeground        Color
	SidebarBorder                  Color
}

// Radius is the corner radius of cards, in pixels at 96 DPI. Smaller
// controls use Radius-4 and Radius-2.
const Radius = 10.0

// Fonts are the font families, in order of preference.
var Fonts = []string{"Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", "Microsoft YaHei"}

// Light is the light theme.
var Light = Palette{
	Background:              oklch(0.985, 0.002, 60, 1),
	Foreground:              oklch(0.2, 0.01, 50, 1),
	Card:                    oklch(1, 0, 0, 1),
	CardForeground:          oklch(0.2, 0.01, 50, 1),
	Popover:                 oklch(1, 0, 0, 1),
	PopoverForeground:       oklch(0.2, 0.01, 50, 1),
	Primary:                 oklch(0.68, 0.17, 38, 1),
	PrimaryForeground:       oklch(0.99, 0, 0, 1),
	Secondary:               oklch(0.955, 0.006, 60, 1),
	SecondaryForeground:     oklch(0.25, 0.01, 50, 1),
	Muted:                   oklch(0.955, 0.006, 60, 1),
	MutedForeground:         oklch(0.52, 0.012, 50, 1),
	Accent:                  oklch(0.95, 0.02, 50, 1),
	AccentForeground:        oklch(0.25, 0.01, 50, 1),
	Destructive:             oklch(0.58, 0.22, 27, 1),
	Border:                  oklch(0.91, 0.006, 60, 1),
	Input:                   oklch(0.89, 0.006, 60, 1),
	Ring:                    oklch(0.68, 0.17, 38, 0.5),
	Sidebar:                 oklch(0.965, 0.004, 60, 1),
	SidebarForeground:       oklch(0.3, 0.01, 50, 1),
	SidebarAccent:           oklch(0.92, 0.008, 60, 1),
	SidebarAccentForeground: oklch(0.2, 0.01, 50, 1),
	SidebarBorder:           oklch(0.91, 0.006, 60, 1),
}

// Dark is the dark theme.
var Dark = Palette{
	Background:              oklch(0.19, 0.004, 50, 1),
	Foreground:              oklch(0.95, 0.004, 60, 1),
	Card:                    oklch(0.23, 0.005, 50, 1),
	CardForeground:          oklch(0.95, 0.004, 60, 1),
	Popover:                 oklch(0.23, 0.005, 50, 1),
	PopoverForeground:       oklch(0.95, 0.004, 60, 1),
	Primary:                 oklch(0.72, 0.16, 40, 1),
	PrimaryForeground:       oklch(0.18, 0.01, 50, 1),
	Secondary:               oklch(0.27, 0.006, 50, 1),
	SecondaryForeground:     oklch(0.95, 0.004, 60, 1),
	Muted:                   oklch(0.27, 0.006, 50, 1),
	MutedForeground:         oklch(0.68, 0.01, 55, 1),
	Accent:                  oklch(0.3, 0.02, 45, 1),
	AccentForeground:        oklch(0.95, 0.004, 60, 1),
	Destructive:             oklch(0.68, 0.19, 25, 1),
	Border:                  oklch(1, 0, 0, 0.09),
	Input:                   oklch(1, 0, 0, 0.14),
	Ring:                    oklch(0.72, 0.16, 40, 0.5),
	Sidebar:                 oklch(0.165, 0.004, 50, 1),
	SidebarForeground:       oklch(0.85, 0.006, 60, 1),
	SidebarAccent:           oklch(0.26, 0.006, 50, 1),
	SidebarAccentForeground: oklch(0.97, 0.004, 60, 1),
	SidebarBorder:           oklch(1, 0, 0, 0.07),
}

// Theme returns the palette for dark or light.
func Theme(dark bool) *Palette {
	if dark {
		return &Dark
	}
	return &Light
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
