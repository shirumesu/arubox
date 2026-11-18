package desktop

import (
	"fmt"
	"slices"

	"github.com/shirumesu/arubox/internal/ui"
)

// The choices the page offers. A size set on the desktop with Ctrl and the
// wheel may lie between them; it is offered too while it is in use.
var (
	iconSizes = []int{32, 40, 48, 56, 64, 72, 80, 96}
	opacities = []int{10, 20, 30, 40, 45, 50, 60, 70, 80, 90}
	// snapLabels name snapModes, in its order.
	snapLabels = []string{"关闭", "留出间距", "紧贴"}
)

// settingsPage is the module's page in the dashboard. The dashboard draws
// the switch that enables the module above it.
type settingsPage struct {
	m *Module
	// errs holds why saving a row's setting failed, by row, until the next
	// change of it succeeds.
	errs map[string]string
}

// Draw draws the appearance and behaviour settings. They are read from the
// module every frame, so a change made on the desktop shows at once.
func (p *settingsPage) Draw(f *ui.Frame, x, y, w float64) float64 {
	s := p.m.currentSettings()
	save := func(row string, next Settings) {
		if err := p.m.setSettings(next); err != nil {
			if p.errs == nil {
				p.errs = map[string]string{}
			}
			p.errs[row] = err.Error()
			return
		}
		delete(p.errs, row)
	}

	y = f.Section(x, y, w, "外观", []ui.Row{
		{
			Title: "图标大小", Description: "也可以在桌面上按住 Ctrl 滚动滚轮调整。", Note: p.errs["iconSize"],
			ControlW: 112,
			Control: func(r ui.Rect) {
				if v, ok := choose(f, "desktop.iconSize", r, iconSizes, s.IconSize, "%d 像素"); ok {
					next := s
					next.IconSize = v
					save("iconSize", next)
				}
			},
		},
		{
			Title: "盒子不透明度", Description: "盒子背景的不透明程度，越低越能透出壁纸。", Note: p.errs["opacity"],
			ControlW: 112,
			Control: func(r ui.Rect) {
				if v, ok := choose(f, "desktop.opacity", r, opacities, s.Opacity, "%d%%"); ok {
					next := s
					next.Opacity = v
					save("opacity", next)
				}
			},
		},
	})
	y += 24

	toggle := func(row, title, description string, get func(*Settings) *bool) ui.Row {
		return ui.Row{
			Title: title, Description: description, Note: p.errs[row],
			ControlW: 36,
			Control: func(r ui.Rect) {
				next := s
				if f.Switch("desktop."+row, r, get(&next), false) {
					save(row, next)
				}
			},
		}
	}
	return f.Section(x, y, w, "行为", []ui.Row{
		toggle("doubleClickHide", "双击桌面隐藏图标", "在桌面空白处双击，隐藏或重新显示所有图标和盒子。",
			func(s *Settings) *bool { return &s.DoubleClickHide }),
		{
			Title: "对齐吸附", Description: "移动或调整盒子时，贴齐屏幕边缘和其他盒子：可以彼此留出间距，也可以紧贴不留缝。", Note: p.errs["snap"],
			ControlW: 112,
			Control: func(r ui.Rect) {
				i := slices.Index(snapModes, s.Snap)
				if f.Select("desktop.snap", r, snapLabels, &i) {
					next := s
					next.Snap = snapModes[i]
					save("snap", next)
				}
			},
		},
		toggle("grid", "网格对齐", "盒子的位置和大小按图标格子对齐，大小总是整数个图标，比如 2×3、5×4。",
			func(s *Settings) *bool { return &s.Grid }),
		toggle("hoverExpand", "悬停展开", "鼠标移到卷起的盒子标题上时临时展开，移开后收起。",
			func(s *Settings) *bool { return &s.HoverExpand }),
	})
}

// choose draws a drop-down of values labelled with format, showing
// current, and reports the value picked when it changes.
func choose(f *ui.Frame, id string, r ui.Rect, values []int, current int, format string) (int, bool) {
	if !slices.Contains(values, current) {
		values = append(slices.Clone(values), current)
		slices.Sort(values)
	}
	labels := make([]string, len(values))
	for i, v := range values {
		labels[i] = fmt.Sprintf(format, v)
	}
	i := slices.Index(values, current)
	if f.Select(id, r, labels, &i) {
		return values[i], true
	}
	return 0, false
}
