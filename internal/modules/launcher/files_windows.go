package launcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/shirumesu/arubox/internal/fileindex"
)

// builtinExcludes are the folders the index always leaves out, besides the
// ones the user adds: what a person rarely looks for by name. They are
// Windows itself, the recycle bins, installed app packages, temporary
// files, and the caches and dependency stores of browsers and developer
// tools. Everything leaves out nothing by default and Windows Search all
// of AppData; this sits between, dropping about half of the entries on a
// developer's machine (by count, which is what the index's memory follows)
// while keeping documents, programs and settings files findable. The
// places come from the environment, so they follow each user and system;
// one that does not exist matches nothing.
func builtinExcludes() []string {
	var list []string
	under := func(env string, parts ...string) {
		if base := os.Getenv(env); base != "" {
			list = append(list, filepath.Join(append([]string{base}, parts...)...))
		}
	}
	under("SystemRoot")
	under("SystemDrive", "Windows.old")
	under("ProgramFiles", "WindowsApps")
	under("ProgramData", "Package Cache")
	under("LOCALAPPDATA", "Temp")
	under("LOCALAPPDATA", "go-build")
	under("LOCALAPPDATA", "npm-cache")
	under("LOCALAPPDATA", ".dartserver")
	under("USERPROFILE", ".cache")
	under("USERPROFILE", "go", "pkg", "mod")
	under("USERPROFILE", ".cargo", "registry")
	under("USERPROFILE", ".nuget", "packages")
	under("USERPROFILE", ".m2", "repository")
	under("USERPROFILE", ".gradle", "caches")
	// Folders of these names, wherever they are.
	return append(list, "$Recycle.Bin", "System Volume Information", "node_modules", ".git", ".pnpm-store",
		"__pycache__", "site-packages", "Cache", "Code Cache", "GPUCache", "Service Worker")
}

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

// search finds the paths of files and folders by a query in Everything's
// syntax, best first.
func (f *fileIndex) search(ctx context.Context, q string, n int) []string {
	return f.client.Search(ctx, q, n)
}

func (f *fileIndex) setExcluded(excluded []string) { f.client.SetExcluded(excluded) }

// loaded is closed once the saved index has been read.
func (f *fileIndex) loaded() <-chan struct{} { return f.client.Loaded() }

func (f *fileIndex) status() FileIndexStatus {
	s := f.client.Status()
	return FileIndexStatus{State: string(s.State), Files: s.Files, Volumes: s.Volumes, Message: s.Message}
}

// install asks for administrator rights and installs the index service.
func (f *fileIndex) install() error { return f.client.Install() }

// remove asks for administrator rights and removes the index service.
func (f *fileIndex) remove() error { return f.client.Remove() }
