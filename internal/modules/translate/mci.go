package translate

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Edge voice arrives as MP3, which MCI plays through the system's
// DirectShow decoder: a few calls into winmm with nothing kept between
// readings, where Media Foundation would need a session and callbacks.

var procMciSendStringW = windows.NewLazySystemDLL("winmm.dll").NewProc("mciSendStringW")

var errPlay = errors.New("无法播放 Edge 在线朗读的声音")

// mci sends one MCI command and returns its answer.
func mci(command string) (string, error) {
	cmd, err := windows.UTF16PtrFromString(command)
	if err != nil {
		return "", err
	}
	var out [128]uint16
	r, _, _ := procMciSendStringW.Call(uintptr(unsafe.Pointer(cmd)), uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), 0)
	if r != 0 {
		return "", errPlay
	}
	return windows.UTF16ToString(out[:]), nil
}

// playMP3 plays the sound and returns when it ends or ctx is cancelled.
// MCI wants a file, so the sound goes through a temporary one.
func playMP3(ctx context.Context, sound []byte) error {
	f, err := os.CreateTemp("", "arubox-speech-*.mp3")
	if err != nil {
		return errPlay
	}
	path := f.Name()
	defer os.Remove(path)
	_, err = f.Write(sound)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return errPlay
	}

	// An MCI device belongs to the thread that opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	alias := "arubox_speech_" + randomHex(4)
	if _, err := mci(`open "` + path + `" type mpegvideo alias ` + alias); err != nil {
		return err
	}
	defer func() { _, _ = mci("close " + alias) }()
	if _, err := mci("play " + alias); err != nil {
		return err
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_, _ = mci("stop " + alias)
			return ctx.Err()
		case <-tick.C:
		}
		mode, err := mci("status " + alias + " mode")
		if err != nil {
			return err
		}
		if mode == "stopped" {
			return nil
		}
	}
}
