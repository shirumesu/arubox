package translate

import (
	"strings"

	"github.com/shirumesu/arubox/internal/ui"
)

const deeplSignup = "https://www.deepl.com/pro-api"

// settingsPage is the module's page in the dashboard. It runs on the UI
// thread, in the dashboard's window.
type settingsPage struct {
	m *Module
	// key is the DeepL key being edited; it is saved when the box loses
	// the focus or Enter is pressed.
	key ui.Field
	// keyEditing is set while key holds an edit not yet saved.
	keyEditing bool
	// errRow names the row whose change failed, and errText says why.
	errRow, errText string
}

const keyBox = "translate.deeplKey"

// Draw draws the settings in the column from x to x+w.
func (p *settingsPage) Draw(f *ui.Frame, x, y, w float64) float64 {
	s := p.m.currentSettings()

	// The key box shows the saved key unless the user is changing it.
	focused := f.FocusedBox() == keyBox
	if p.key.Changed {
		p.key.Changed = false
		p.keyEditing = true
	}
	if ev := f.Event(); ev.Kind == ui.EvKeyDown && ev.ID == keyBox && ev.Key == ui.KeyReturn && ev.Mods == 0 {
		f.TakeKey()
		f.Blur()
		focused = false
	}
	if p.keyEditing && !focused {
		p.keyEditing = false
		key := strings.TrimSpace(p.key.Text)
		if key != s.DeepLKey {
			p.save(f, "deeplKey", func(s *Settings) {
				s.DeepLKey = key
				// Without a key DeepL cannot stay the engine.
				if key == "" && s.Engine == engineDeepL {
					s.Engine = engineGoogle
				}
			})
			s = p.m.currentSettings()
		}
	}
	if !p.keyEditing {
		p.key.Text = s.DeepLKey
	}
	p.key.Password, p.key.Placeholder, p.key.Size = true, "未填写", 13

	sel := func(row, id string, opts []option, value string, set func(*Settings, string)) func(r ui.Rect) {
		return func(r ui.Rect) {
			i := optionIndex(opts, value)
			if f.Select(id, r, optionLabels(opts), &i) && i >= 0 {
				v := opts[i].value
				p.save(f, row, func(s *Settings) { set(s, v) })
			}
		}
	}
	note := func(row string) string {
		if p.errRow == row {
			return p.errText
		}
		return ""
	}

	y = f.Section(x, y, w, "语言", []ui.Row{
		{
			Title: "原文语言", Description: "一般选自动检测。浮窗里也可以随时切换。", Note: note("source"), ControlW: 128,
			Control: sel("source", "translate.source", languageOptions(s.Source, true, ""), s.Source,
				func(s *Settings, v string) { s.Source = v }),
		},
		{
			Title: "目标语言", Description: "译成这种语言。", Note: note("target"), ControlW: 128,
			Control: sel("target", "translate.target", languageOptions(s.Target, false, ""), s.Target,
				func(s *Settings, v string) { s.Target = v }),
		},
		{
			Title: "第二目标语言", Description: "自动检测到原文已经是目标语言时，改译成这种语言。", Note: note("second"), ControlW: 128,
			Control: sel("second", "translate.second", languageOptions(s.Second, false, ""), s.Second,
				func(s *Settings, v string) { s.Second = v }),
		},
	})
	y += 24

	get := ui.Button{Label: "获取", Icon: ui.IconLink, Small: true}
	getW := get.Width()
	y = f.Section(x, y, w, "翻译服务", []ui.Row{
		{
			Title:       "翻译服务",
			Description: "Google 翻译和 Edge 翻译免费、无需设置，偶尔可能用不了。浮窗里也可以随时切换。DeepL 要先填写下面的密钥。",
			Note:        note("engine"), ControlW: 128,
			Control: sel("engine", "translate.engine", engineOptions(s), s.Engine,
				func(s *Settings, v string) { s.Engine = v }),
		},
		{
			Title:       "DeepL API 密钥",
			Description: "注册 DeepL API Free 即可免费获取，每月 50 万字符。密钥只保存在本机的配置文件里。",
			Note:        note("deeplKey"), ControlW: 224 + 8 + getW,
			Control: func(r ui.Rect) {
				box := ui.Rect{X: r.X, Y: r.Y, W: 224, H: r.H}
				t := f.Theme
				f.FillRound(box, 6, t.Card)
				f.StrokeRound(box, 6, 1, t.Input)
				f.TextBox(keyBox, box.InsetXY(8, 2), &p.key, t.Card)
				if f.Button("translate.deeplGet", ui.Rect{X: box.Right() + 8, Y: r.Y, W: getW, H: r.H}, get) {
					_ = ui.OpenURL(deeplSignup)
				}
			},
		},
	})
	y += 24

	y = f.Section(x, y, w, "朗读", []ui.Row{
		{
			Title:       "朗读声音",
			Description: "系统声音不用联网。Edge 的声音更自然，但需要联网，偶尔可能用不了。",
			Note:        note("voice"), ControlW: 144,
			Control: sel("voice", "translate.voice", voiceOptions, s.Voice,
				func(s *Settings, v string) { s.Voice = v }),
		},
	})
	return y
}

// save applies a change to the settings, and shows why on row when it is
// refused.
func (p *settingsPage) save(f *ui.Frame, row string, change func(*Settings)) {
	s := p.m.currentSettings()
	change(&s)
	if err := p.m.setSettings(s); err != nil {
		p.errRow, p.errText = row, err.Error()
	} else {
		p.errRow, p.errText = "", ""
	}
	f.Invalidate()
}
