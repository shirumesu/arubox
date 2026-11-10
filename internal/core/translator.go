package core

import (
	"context"
	"errors"

	"github.com/shirumesu/arubox/internal/modules/launcher"
	"github.com/shirumesu/arubox/internal/modules/translate"
)

// launcherTranslator lets the launcher translate through the translation
// module, so that neither module depends on the other.
type launcherTranslator struct {
	module *translate.Module
}

func (t launcherTranslator) Translate(ctx context.Context, text string) (launcher.Translation, bool, error) {
	r, engine, err := t.module.TranslateText(ctx, text)
	if errors.Is(err, translate.ErrDisabled) {
		return launcher.Translation{}, false, nil
	}
	return launcher.Translation{Text: r.Text, Source: r.Source, Target: r.Target, Engine: engine}, true, err
}

func (t launcherTranslator) Open(text string) {
	t.module.OpenText(text)
}
