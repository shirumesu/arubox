package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// stallTimeout is how long a download may wait for its next bytes.
const stallTimeout = 30 * time.Second

// download writes a package to path and checks it against the manifest's
// size and digest. progress gets the bytes received so far. The URLs are
// copies of the same file; the next is tried when one fails, and the
// error names every failure.
func download(ctx context.Context, client *http.Client, file packageFile, path string, progress func(received int64)) error {
	var failures []error
	for _, address := range file.URLs {
		err := downloadFrom(ctx, client, address, file, path, progress)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func downloadFrom(ctx context.Context, client *http.Client, address string, file packageFile, path string, progress func(int64)) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	errStalled := fmt.Errorf("下载更新包超过 %d 秒没有收到数据", int(stallTimeout.Seconds()))
	stall := time.AfterFunc(stallTimeout, func() { cancel(errStalled) })
	defer stall.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		if cause := context.Cause(ctx); errors.Is(cause, errStalled) {
			return cause
		}
		return fmt.Errorf("无法连接 %s：%w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s 返回 %s", address, resp.Status)
	}

	out, err := os.Create(path)
	if err != nil {
		return writeFailure(path, err)
	}
	hash := sha256.New()
	var received int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			received += int64(n)
			if received > file.Size {
				out.Close()
				return errors.New("更新包比更新清单写的大，可能不是这次发布的文件")
			}
			if _, err := out.Write(buf[:n]); err != nil {
				out.Close()
				return writeFailure(path, err)
			}
			hash.Write(buf[:n])
			progress(received)
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			out.Close()
			if cause := context.Cause(ctx); errors.Is(cause, errStalled) {
				return cause
			}
			return fmt.Errorf("下载更新包中断：%w", rerr)
		}
	}
	if err := out.Close(); err != nil {
		return writeFailure(path, err)
	}
	if received != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return errors.New("更新包校验失败，可能下载不完整，请重试")
	}
	return nil
}

func writeFailure(path string, err error) error {
	return fmt.Errorf("无法写入 %s：%w", path, err)
}
