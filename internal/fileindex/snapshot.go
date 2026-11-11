//go:build windows

package fileindex

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"unsafe"
)

// The index is saved to disk, so the next start loads it in a moment and
// catches up from the change journal instead of reading every volume's
// file table again.

const snapshotMagic = "ARUFIDX3"

// saveSnapshot writes the volumes to path, through a temporary file so a
// crash mid-write leaves the previous snapshot.
func saveSnapshot(path string, excludeList []string, vols []*volume) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	err = writeSnapshot(w, excludeList, vols)
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func writeSnapshot(w io.Writer, excludeList []string, vols []*volume) error {
	var e encoder
	e.b = append(e.b, snapshotMagic...)
	e.u16(uint16(len(excludeList)))
	for _, x := range excludeList {
		e.str(x)
	}
	e.u16(uint16(len(vols)))
	if _, err := w.Write(e.b); err != nil {
		return err
	}
	for _, v := range vols {
		v.mu.RLock()
		err := writeVolume(w, v)
		v.mu.RUnlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeVolume(w io.Writer, v *volume) error {
	var e encoder
	e.str(v.guid)
	e.str(v.mount)
	e.u64(v.journalID)
	e.u64(uint64(v.nextUSN))
	e.u32(uint32(len(v.off)))
	e.u32(uint32(len(v.names)))
	e.u32(uint32(len(v.casedAt)))
	e.u32(uint32(len(v.cased)))
	if _, err := w.Write(e.b); err != nil {
		return err
	}
	for _, data := range v.arrays() {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// arrays returns the volume's arrays as their bytes in memory, which is
// how they are saved: every platform the program runs on is little-endian,
// and going through encoding/binary would copy each one first.
func (v *volume) arrays() [][]byte {
	return [][]byte{
		raw(v.off), raw(v.parent), raw(v.rec), raw(v.seq), v.flags, v.names,
		raw(v.casedAt), raw(v.casedOff), v.cased,
	}
}

func raw[E uint16 | uint32](s []E) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*int(unsafe.Sizeof(s[0])))
}

// loadSnapshot reads the volumes saved at path. A snapshot saved with
// other excluded folders is no use, since it holds what is now excluded or
// lacks what no longer is.
func loadSnapshot(path string, excludeList []string) ([]*volume, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	magic := make([]byte, len(snapshotMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != snapshotMagic {
		return nil, errors.New("not an index snapshot")
	}
	var saved []string
	n, err := readU16(r)
	for ; err == nil && n > 0; n-- {
		var s string
		s, err = readString(r)
		saved = append(saved, s)
	}
	if err != nil {
		return nil, err
	}
	if !slices.Equal(saved, excludeList) {
		return nil, errors.New("excluded folders changed")
	}
	count, err := readU16(r)
	if err != nil {
		return nil, err
	}
	vols := make([]*volume, 0, count)
	for range count {
		v, err := readVolume(r)
		if err != nil {
			return nil, fmt.Errorf("read volume: %w", err)
		}
		vols = append(vols, v)
	}
	return vols, nil
}

func readVolume(r io.Reader) (*volume, error) {
	guid, err := readString(r)
	if err != nil {
		return nil, err
	}
	mount, err := readString(r)
	if err != nil {
		return nil, err
	}
	var head struct {
		JournalID  uint64
		NextUSN    int64
		Entries    uint32
		Names      uint32
		Cased      uint32
		CasedNames uint32
	}
	if err := binary.Read(r, binary.LittleEndian, &head); err != nil {
		return nil, err
	}
	v := newVolume(guid, mount, head.JournalID)
	v.nextUSN = head.NextUSN
	n, c := int(head.Entries), int(head.Cased)
	v.off, v.parent, v.rec = make([]uint32, n), make([]uint32, n), make([]uint32, n)
	v.seq, v.flags = make([]uint16, n), make([]byte, n)
	v.names = make([]byte, head.Names)
	v.casedAt, v.casedOff = make([]uint32, c), make([]uint32, c)
	v.cased = make([]byte, head.CasedNames)
	for _, data := range v.arrays() {
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}
	}
	for _, f := range v.flags {
		if f&flagDead != 0 {
			v.dead++
		}
	}
	v.reindex()
	return v, nil
}

func readU16(r io.Reader) (uint16, error) {
	var b [2]byte
	_, err := io.ReadFull(r, b[:])
	return binary.LittleEndian.Uint16(b[:]), err
}

func readString(r io.Reader) (string, error) {
	n, err := readU16(r)
	if err != nil {
		return "", err
	}
	b := make([]byte, n)
	_, err = io.ReadFull(r, b)
	return string(b), err
}
