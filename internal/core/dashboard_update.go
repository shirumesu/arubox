package core

import (
	"fmt"
	"math"
	"strings"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/ui"
	"github.com/shirumesu/arubox/internal/update"
)

// noteKind is what a line of release notes is.
type noteKind int

const (
	noteHeading    noteKind = iota // "## "
	noteSubheading                 // "### "
	noteBullet                     // "- "
	noteParagraph
)

type noteBlock struct {
	kind noteKind
	text string
}

// parseNotes reads release notes, which follow the CHANGELOG.md
// convention: "##" and "###" headings over "- " bullets, one line each;
// any other line is a paragraph.
func parseNotes(notes string) []noteBlock {
	var blocks []noteBlock
	for _, line := range strings.Split(notes, "\n") {
		text := strings.TrimSpace(line)
		switch {
		case text == "":
		case strings.HasPrefix(text, "- "):
			blocks = append(blocks, noteBlock{noteBullet, text[2:]})
		case strings.HasPrefix(text, "### "):
			blocks = append(blocks, noteBlock{noteSubheading, strings.TrimSpace(text[4:])})
		case strings.HasPrefix(text, "## "):
			blocks = append(blocks, noteBlock{noteHeading, strings.TrimSpace(text[3:])})
		default:
			blocks = append(blocks, noteBlock{noteParagraph, text})
		}
	}
	return blocks
}

// layoutNotes lays the notes out from y in the column x..x+w, drawing them
// when f is not nil, and returns their height.
func layoutNotes(f *ui.Frame, t *design.Palette, blocks []noteBlock, x, y, w float64) float64 {
	top := y
	for i, b := range blocks {
		style := ui.Text{Size: 13, Wrap: true, LineHeight: 21, Color: t.Foreground}
		bx, bw := x, w
		if i > 0 {
			// Bullets of one list sit closer together than other blocks.
			if b.kind == noteBullet && blocks[i-1].kind == noteBullet {
				y += 4
			} else {
				y += 8
			}
		}
		switch b.kind {
		case noteHeading:
			style.Weight = ui.Semibold
			if i > 0 {
				y += 8
			}
		case noteSubheading:
			style.Weight, style.Color = ui.Medium, t.MutedForeground
			if i > 0 {
				y += 4
			}
		case noteBullet:
			bx, bw = x+20, w-20
		}
		_, h := ui.MeasureText(b.text, style, bw)
		if f != nil {
			if b.kind == noteBullet {
				f.FillEllipse(ui.Rect{X: x + 7, Y: y + 8, W: 5, H: 5}, t.Foreground)
			}
			f.Text(ui.Rect{X: bx, Y: y, W: bw, H: h}, b.text, style)
		}
		y += h
	}
	return y - top
}

// drawUpdateDialog shows the release on offer, installs it once confirmed
// and follows the installation.
func (d *dashboard) drawUpdateDialog(f *ui.Frame) {
	s := d.update
	rel := s.Release
	if !d.showUpdate || rel == nil {
		return
	}
	t := f.Theme
	if d.notesOf != rel.Notes {
		d.notes, d.notesOf = parseNotes(rel.Notes), rel.Notes
	}
	const boxW = 576
	inner := boxW - 48.0
	muted := ui.Text{Size: 13, Wrap: true, Color: t.MutedForeground}

	notesH := 0.0
	if len(d.notes) > 0 {
		notesH = layoutNotes(nil, t, d.notes, 0, 0, inner-32)
	}
	const elevation = "文件搜索会一起更新，需要管理员授权一次。"
	var elevH, errH float64
	if s.Elevation {
		_, elevH = ui.MeasureText(elevation, muted, inner)
	}
	if s.Error != "" {
		_, errH = ui.MeasureText(s.Error, muted, inner)
	}

	// Everything but the notes, which scroll in what is left.
	fixed := 48 + 24 + 4 + 18 + 16.0
	if s.Elevation {
		fixed += elevH + 16
	}
	if s.Phase == update.PhaseDownloading {
		fixed += 8 + 6 + 18 + 16
	}
	if s.Error != "" {
		fixed += errH + 16
	}
	fixed += 36
	boxH := 0.0
	if len(d.notes) > 0 {
		_, wh := f.Size()
		boxH = math.Min(320, notesH+24)
		boxH = math.Max(80, math.Min(boxH, wh-32-fixed-16))
	}
	h := fixed
	if boxH > 0 {
		h += boxH + 16
	}

	working := s.Phase == update.PhaseDownloading || s.Phase == update.PhaseInstalling || s.Phase == update.PhaseRestarting
	dismissed := f.Dialog("update", boxW, h, func(r ui.Rect) {
		y := r.Y
		f.Text(ui.Rect{X: r.X, Y: y, W: r.W, H: 24}, "arubox "+rel.Version, ui.Text{Size: 18, Weight: ui.Semibold, Color: t.Foreground, Middle: true})
		y += 28
		f.Text(ui.Rect{X: r.X, Y: y, W: r.W, H: 18}, "正在使用 "+s.Version, ui.Text{Size: 13, Color: t.MutedForeground, Middle: true})
		y += 18 + 16

		if boxH > 0 {
			box := ui.Rect{X: r.X, Y: y, W: r.W, H: boxH}
			f.FillRound(box, design.Radius-3, t.Card)
			f.StrokeRound(box, design.Radius-3, 1, t.Border)
			f.Scroll("update.notes", box.Inset(1), notesH+22, func(a ui.Rect) {
				layoutNotes(f, t, d.notes, a.X+15, a.Y+11, a.W-30)
			})
			y += boxH + 16
		}
		if s.Elevation {
			f.Text(ui.Rect{X: r.X, Y: y, W: r.W, H: elevH}, elevation, muted)
			y += elevH + 16
		}
		if s.Phase == update.PhaseDownloading {
			frac := 0.0
			if s.Total > 0 {
				frac = float64(s.Received) / float64(s.Total)
			}
			f.Progress(ui.Rect{X: r.X, Y: y + 2, W: r.W, H: 6}, frac)
			y += 8 + 6
			mib := func(b int64) float64 { return float64(b) / (1 << 20) }
			f.Text(ui.Rect{X: r.X, Y: y, W: r.W, H: 18}, fmt.Sprintf("正在下载 %.1f / %.1f MB", mib(s.Received), mib(s.Total)),
				ui.Text{Size: 13, Color: t.MutedForeground, Middle: true})
			y += 18 + 16
		}
		if s.Error != "" {
			e := muted
			e.Color = t.Destructive
			f.Text(ui.Rect{X: r.X, Y: y, W: r.W, H: errH}, s.Error, e)
		}

		by := r.Bottom() - 36
		if working {
			label := "正在下载"
			switch s.Phase {
			case update.PhaseRestarting:
				label = "正在重启"
			case update.PhaseInstalling:
				label = "正在安装"
			}
			b := ui.Button{Label: label, Kind: ui.ButtonPrimary, Disabled: true}
			bw := b.Width()
			f.Button("update.working", ui.Rect{X: r.Right() - bw, Y: by, W: bw, H: 36}, b)
			return
		}
		install := ui.Button{Label: "更新并重启", Kind: ui.ButtonPrimary, Disabled: s.Phase != update.PhaseAvailable}
		later := ui.Button{Label: "稍后"}
		iw, lw := install.Width(), later.Width()
		if f.Button("update.later", ui.Rect{X: r.Right() - iw - 8 - lw, Y: by, W: lw, H: 36}, later) {
			d.showUpdate = false
		}
		if f.Button("update.install", ui.Rect{X: r.Right() - iw, Y: by, W: iw, H: 36}, install) {
			// Progress and failures arrive through the updater's OnChange.
			go d.core.updater.Install()
		}
	})
	if dismissed || f.KeyPressed(ui.KeyEscape, 0) {
		d.showUpdate = false
	}
}
