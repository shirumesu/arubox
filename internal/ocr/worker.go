package ocr

import (
	"bufio"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/shirumesu/arubox/internal/platform"
)

// PP-OCRv6 runs in a worker: arubox's own executable started with workerArg
// and the engine's folder, reading pictures from stdin and writing the lines
// read to stdout. A picture goes as a line with its width and height, then
// its pixels as they are, so that neither side copies them; replies go as
// gob. Releasing the model ends the process, which gives back all of its
// memory: ONNX Runtime keeps tens of megabytes after its
// environment is released, the macOS allocator keeps the large blocks a run
// frees, and Windows cannot delete a DLL that is loaded. A crash in ONNX
// Runtime ends the worker, not arubox.
const workerArg = "--ocr-worker"

// reply is the lines read, or why not. The worker's first reply, before any
// request, tells whether the model loaded.
type reply struct {
	Lines []platform.TextLine
	Err   string
}

// RunWorker runs the worker if the command line asks for it, and reports
// whether it did.
func RunWorker(args []string) bool {
	if len(args) != 3 || args[1] != workerArg {
		return false
	}
	if err := serve(args[2], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}

// serve loads the model from dir and reads pictures until in closes.
func serve(dir string, in io.Reader, out io.Writer) error {
	enc := gob.NewEncoder(out)
	p, err := loadPaddle(filesIn(dir, assets()))
	if err != nil {
		return enc.Encode(reply{Err: err.Error()})
	}
	if err := enc.Encode(reply{}); err != nil {
		return err
	}
	r := bufio.NewReader(in)
	for {
		var img platform.Image
		if _, err := fmt.Fscanf(r, "%d %d\n", &img.Width, &img.Height); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		img.Pixels = make([]byte, img.Width*img.Height*4)
		if _, err := io.ReadFull(r, img.Pixels); err != nil {
			return err
		}
		lines, err := p.read(img)
		res := reply{Lines: lines}
		if err != nil {
			res.Err = err.Error()
		}
		if err := enc.Encode(res); err != nil {
			return err
		}
		// The picture and its copies are garbage now; between uses the
		// worker should hold only the models.
		debug.FreeOSMemory()
	}
}

// workerEnv is the worker's environment. On macOS it keeps ONNX Runtime's
// telemetry from starting, and turns off the allocator's cache of freed
// large blocks: a run allocates and frees a few hundred megabytes, and the
// cache would keep them while the worker idles. MallocLargeCache is read by
// libmalloc but not documented; should it stop working, the worker holds
// more memory while it runs, and still gives it all back when it ends.
func workerEnv() []string {
	env := os.Environ()
	if runtime.GOOS == "darwin" {
		env = append(env, "ORT_DISABLE_TELEMETRY=1", "MallocLargeCache=0")
	}
	return env
}

// worker is a running worker process.
type worker struct {
	cmd   *exec.Cmd
	stdin io.Writer
	dec   *gob.Decoder
}

// startWorker starts a worker on the files in dir and waits until it has
// loaded the model.
func startWorker(dir string, log *slog.Logger) (*worker, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, workerArg, dir)
	cmd.Env = workerEnv()
	cmd.Stderr = logWriter{log}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 PP-OCRv6 进程失败：%w", err)
	}
	w := &worker{cmd: cmd, stdin: stdin, dec: gob.NewDecoder(stdout)}
	var r reply
	if err := w.dec.Decode(&r); err != nil {
		return nil, w.died()
	}
	if r.Err != "" {
		w.stop()
		return nil, errors.New(r.Err)
	}
	return w, nil
}

// read has the worker read the text in img. When the worker has ended, the
// error wraps errWorkerDied, and the worker must not be used again.
func (w *worker) read(img platform.Image) ([]platform.TextLine, error) {
	if _, err := fmt.Fprintf(w.stdin, "%d %d\n", img.Width, img.Height); err != nil {
		return nil, w.died()
	}
	if _, err := w.stdin.Write(img.Pixels[:img.Width*img.Height*4]); err != nil {
		return nil, w.died()
	}
	var r reply
	if err := w.dec.Decode(&r); err != nil {
		return nil, w.died()
	}
	if r.Err != "" {
		return nil, errors.New(r.Err)
	}
	return r.Lines, nil
}

var errWorkerDied = errors.New("PP-OCRv6 进程意外退出")

// died waits for a worker that stopped answering and says how it ended.
func (w *worker) died() error {
	w.stop()
	return fmt.Errorf("%w（%s）", errWorkerDied, w.cmd.ProcessState)
}

// stop ends the worker. It holds nothing that needs saving.
func (w *worker) stop() {
	// Killing fails only for a process that has already ended, which Wait
	// then collects.
	_ = w.cmd.Process.Kill()
	_ = w.cmd.Wait()
}

// logWriter logs what the worker writes to stderr: ONNX Runtime's errors,
// and a Go panic's trace.
type logWriter struct {
	log *slog.Logger
}

func (l logWriter) Write(p []byte) (int, error) {
	l.log.Warn("ocr worker", "output", strings.TrimSpace(string(p)))
	return len(p), nil
}
