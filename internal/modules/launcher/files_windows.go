package launcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/shirumesu/arubox/internal/fileindex"
	"github.com/shirumesu/arubox/internal/platform"
)

// defaultFileExcludes keeps out what a person rarely looks for: the system
// folder, the recycle bins and the folders of dependencies and version
// control.
var defaultFileExcludes = []string{`C:\Windows`, "$Recycle.Bin", "System Volume Information", "node_modules", ".git"}

// fileIndex is the launcher's own index of every NTFS volume, kept by a
// service with administrator rights.
type fileIndex struct {
	client *fileindex.Client
}

func newFileIndex(log *slog.Logger, excluded []string) (*fileIndex, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	c := fileindex.NewClient(log, filepath.Join(dir, "arubox", "fileindex.bin"), excluded)
	if err := c.Start(); err != nil {
		return nil, err
	}
	return &fileIndex{client: c}, nil
}

// stop disconnects from the service and saves the index.
func (f *fileIndex) stop() { f.client.Stop() }

// search finds files and folders by a query in Everything's syntax, best
// first.
func (f *fileIndex) search(ctx context.Context, q string, n int) []platform.FoundFile {
	paths := f.client.Search(ctx, q, n)
	out := make([]platform.FoundFile, len(paths))
	for i, p := range paths {
		out[i] = platform.FoundFile{Path: p}
	}
	return out
}

func (f *fileIndex) setExcluded(excluded []string) { f.client.SetExcluded(excluded) }

func (f *fileIndex) status() FileIndexStatus {
	s := f.client.Status()
	return FileIndexStatus{State: string(s.State), Files: s.Files, Volumes: s.Volumes, Message: s.Message}
}

// install asks for administrator rights and installs the index service.
func (f *fileIndex) install() error { return f.client.Install() }

// remove asks for administrator rights and removes the index service.
func (f *fileIndex) remove() error { return f.client.Remove() }
