package launcher

import (
	"context"
	"errors"
	"log/slog"

	"github.com/shirumesu/arubox/internal/platform"
)

// defaultFileExcludes is empty: Spotlight's index leaves out what it
// should, and searchFiles drops the rest.
var defaultFileExcludes []string

// errSpotlight is returned for what only the Windows index has.
var errSpotlight = errors.New("macOS 用聚焦搜索的索引，不需要另外安装")

// fileIndex searches files through Spotlight, which keeps the index
// itself.
type fileIndex struct{}

func newFileIndex(*slog.Logger, []string) (*fileIndex, error) { return &fileIndex{}, nil }

func (*fileIndex) stop() {}

func (*fileIndex) search(_ context.Context, q string, n int) []platform.FoundFile {
	return platform.SearchFiles(q, n)
}

func (*fileIndex) setExcluded([]string) {}

func (*fileIndex) status() FileIndexStatus { return FileIndexStatus{State: "spotlight"} }

func (*fileIndex) install() error { return errSpotlight }

func (*fileIndex) remove() error { return errSpotlight }
