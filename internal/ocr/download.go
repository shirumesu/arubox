package ocr

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// stallTimeout gives up on a source that sends nothing for this long, so
// the next one is tried.
const stallTimeout = 30 * time.Second

// Progress is how a download is going, or how it ended.
type Progress struct {
	Running bool `json:"running"`
	// File and Source name what is being fetched and from where.
	File   string `json:"file"`
	Source string `json:"source"`
	// Done and Total count bytes of every file to fetch.
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
	// Failures are the sources that failed, in the order tried.
	Failures []Failure `json:"failures"`
	// Error is why the download stopped, if it did not finish.
	Error string `json:"error,omitempty"`
}

// Failure is a source that could not deliver a file.
type Failure struct {
	File   string `json:"file"`
	Source string `json:"source"`
	Error  string `json:"error"`
}

// fetch is a download of every file not yet in place.
type fetch struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	p      Progress
}

func (f *fetch) progress() Progress {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.p
	p.Failures = slices.Clone(f.p.Failures)
	return p
}

func (f *fetch) update(fn func(p *Progress)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(&f.p)
}

var errCanceled = errors.New("下载已取消")

// startDownload fetches the files that are not in place, in the
// background. Files already placed are kept, so a download cancelled or
// failed half way goes on where it stopped.
func (e *Engine) startDownload() error {
	if e.assets == nil {
		return errUnsupported
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fetch != nil && e.fetch.progress().Running {
		return nil
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return fmt.Errorf("创建下载目录失败：%w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &fetch{cancel: cancel}
	f.p.Running = true
	f.p.Failures = []Failure{}
	for _, a := range e.assets {
		f.p.Total += a.download.size
		if e.placed(a) {
			f.p.Done += a.download.size
		}
	}
	e.fetch = f
	go func() {
		defer cancel()
		err := e.downloadAll(ctx, f)
		f.update(func(p *Progress) {
			p.Running = false
			p.File, p.Source = "", ""
			if err != nil {
				p.Error = err.Error()
			}
		})
		if err != nil && !errors.Is(err, errCanceled) {
			e.log.Error("download PP-OCRv6", "error", err)
		} else if err == nil {
			e.log.Info("PP-OCRv6 downloaded")
		}
	}()
	return nil
}

// cancelDownload stops the running download, if any.
func (e *Engine) cancelDownload() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fetch != nil {
		e.fetch.cancel()
	}
}

// downloading reports whether a download is running.
func (e *Engine) downloading() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fetch != nil && e.fetch.progress().Running
}

func (e *Engine) placed(a asset) bool {
	info, err := os.Stat(filepath.Join(e.dir, a.name))
	return err == nil && info.Size() == a.size
}

func (e *Engine) downloadAll(ctx context.Context, f *fetch) error {
	for _, a := range e.assets {
		if e.placed(a) {
			continue
		}
		if err := e.downloadAsset(ctx, f, a); err != nil {
			return err
		}
	}
	return nil
}

// downloadAsset fetches a file from its sources in order until one
// delivers it intact.
func (e *Engine) downloadAsset(ctx context.Context, f *fetch, a asset) error {
	var done int64
	f.update(func(p *Progress) { done = p.Done })
	for _, src := range a.download.sources {
		f.update(func(p *Progress) {
			p.File, p.Source, p.Done = a.label, src.Name, done
		})
		err := e.fetchFrom(ctx, f, a, src)
		if err == nil {
			f.update(func(p *Progress) { p.Done = done + a.download.size })
			return nil
		}
		if ctx.Err() != nil {
			return errCanceled
		}
		e.log.Warn("download source failed", "file", a.name, "source", src.Name, "error", err)
		f.update(func(p *Progress) {
			p.Failures = append(p.Failures, Failure{File: a.label, Source: src.Name, Error: err.Error()})
		})
	}
	return fmt.Errorf("%s下载失败：所有来源都不可用", a.label)
}

// fetchFrom downloads a file from one source, checks it and puts it in
// place. Nothing reaches the file's own name unless it is intact.
func (e *Engine) fetchFrom(ctx context.Context, f *fetch, a asset, src Source) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return describe(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回 %s", resp.Status)
	}

	tmp := filepath.Join(e.dir, a.name+".download")
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // fails harmlessly once renamed
	stall := time.AfterFunc(stallTimeout, func() { cancel(errStalled) })
	defer stall.Stop()
	hash := sha256.New()
	var start int64
	f.update(func(p *Progress) { start = p.Done })
	var got int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			got += int64(n)
			if got > a.download.size {
				out.Close()
				return errors.New("文件比预期的大")
			}
			hash.Write(buf[:n])
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return werr
			}
			f.update(func(p *Progress) { p.Done = start + got })
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			out.Close()
			return describe(ctx, rerr)
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if got != a.download.size {
		return errors.New("文件不完整")
	}
	if hex.EncodeToString(hash.Sum(nil)) != a.download.sha256 {
		return errors.New("校验和不符")
	}
	final := filepath.Join(e.dir, a.name)
	if a.member == "" {
		return os.Rename(tmp, final)
	}
	return extract(tmp, a, final)
}

// extract places a's member of the zip archive at path.
func extract(archive string, a asset, path string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, entry := range z.File {
		if entry.Name != a.member {
			continue
		}
		if entry.FileInfo().Size() != a.size {
			return fmt.Errorf("压缩包里的 %s 大小不符", a.member)
		}
		in, err := entry.Open()
		if err != nil {
			return err
		}
		defer in.Close()
		tmp := path + ".part"
		out, err := os.Create(tmp)
		if err != nil {
			return err
		}
		defer os.Remove(tmp) // fails harmlessly once renamed
		if _, err := io.CopyN(out, in, a.size); err != nil {
			out.Close()
			return err
		}
		// Reading on to the end has the reader check the entry's CRC.
		switch n, err := in.Read(make([]byte, 1)); {
		case n > 0:
			out.Close()
			return fmt.Errorf("压缩包里的 %s 大小不符", a.member)
		case !errors.Is(err, io.EOF):
			out.Close()
			return fmt.Errorf("压缩包里的 %s 已损坏：%w", a.member, err)
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	return fmt.Errorf("压缩包里没有 %s", a.member)
}

var errStalled = errors.New("连接停滞")

// describe names why a request failed: a stall, or the error itself
// without the URL, which the source's name stands for.
func describe(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, errStalled) {
		return fmt.Errorf("%d 秒没有收到数据", int(stallTimeout.Seconds()))
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// remove deletes every file of the engine.
func (e *Engine) remove() error {
	if err := os.RemoveAll(e.dir); err != nil {
		return fmt.Errorf("删除失败：%w", err)
	}
	return nil
}
