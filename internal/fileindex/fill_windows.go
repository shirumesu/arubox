package fileindex

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fill lists what is inside folders that came into the index from outside
// it, such as one moved out of an excluded folder or a temporary one, or
// back from the Recycle Bin: the journal tells of the folder only, not of
// what it holds. The folders are read as the user, one at a time, without
// holding the volume's lock while the disk is read. What is already in the
// index is left as it is, and so is what is inside it.
func (c *Client) fill(v *volume, folders []uint32, ex *excludes) {
	queue := folders
	listed, failed := 0, 0
	var firstErr error
	for len(queue) > 0 {
		select {
		case <-c.quit:
			return
		default:
		}
		rec := queue[0]
		queue = queue[1:]
		v.mu.RLock()
		path, ok := "", v.indexedFolder(rec)
		if ok {
			path, ok = v.path(v.lookup(rec), false)
		}
		v.mu.RUnlock()
		if !ok {
			// Gone, or moved out again, since.
			continue
		}
		entries, err := readFolder(path)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		v.mu.Lock()
		for _, e := range entries {
			if v.lookup(e.rec) >= 0 {
				continue
			}
			v.set(e.rec, e.seq, e.flags, []link{{parent: rec, name: e.name}}, ex)
			listed++
			if e.flags&flagDir != 0 && !e.link && v.indexedFolder(e.rec) {
				queue = append(queue, e.rec)
			}
		}
		v.mu.Unlock()
	}
	if failed > 0 {
		c.log.Warn("list folders moved into the index", "volume", v.mount, "failed", failed, "error", firstErr)
	}
	c.log.Info("listed folders moved into the index", "volume", v.mount, "folders", len(folders), "entries", listed)
}

// folderEntry is one file or folder in a folder, as readFolder finds it.
type folderEntry struct {
	rec   uint32
	seq   uint16
	flags byte
	name  string
	// link is set for a junction or symbolic link to a folder, whose
	// contents belong to its target, perhaps on another volume.
	link bool
}

const (
	fileAttributeHidden       = 0x2
	fileAttributeSystem       = 0x4
	fileAttributeDirectory    = 0x10
	fileAttributeReparsePoint = 0x400
)

// readFolder lists a folder with each entry's file reference number, as
// FILE_ID_BOTH_DIR_INFO gives them.
func readFolder(path string) ([]folderEntry, error) {
	long := path
	if !strings.HasPrefix(long, `\\?\`) {
		long = `\\?\` + long
	}
	name, err := windows.UTF16PtrFromString(long)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer closeHandle(h)
	le := binary.LittleEndian
	// The entries hold 64-bit fields, so the buffer must be 8-byte
	// aligned; a byte slice need not be, and the call then fails with
	// ERROR_NOACCESS.
	words := make([]uint64, 8<<10)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*8)
	var out []folderEntry
	for {
		err := windows.GetFileInformationByHandleEx(h, windows.FileIdBothDirectoryInfo, &buf[0], uint32(len(buf)))
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", path, err)
		}
		// FILE_ID_BOTH_DIR_INFO: NextEntryOffset at 0, FileAttributes at
		// 56, FileNameLength (bytes) at 60, FileId at 96, FileName at 104.
		for off := 0; ; {
			e := buf[off:]
			attrs, n, id := le.Uint32(e[56:]), int(le.Uint32(e[60:])), le.Uint64(e[96:])
			u := unsafe.Slice((*uint16)(unsafe.Pointer(&e[104])), n/2)
			if s := string(utf16.Decode(u)); s != "." && s != ".." {
				var flags byte
				if attrs&fileAttributeDirectory != 0 {
					flags |= flagDir
				}
				if attrs&fileAttributeHidden != 0 {
					flags |= flagHidden
				}
				if attrs&fileAttributeSystem != 0 {
					flags |= flagSystem
				}
				out = append(out, folderEntry{rec: recordNumber(id), seq: sequence(id), flags: flags, name: s,
					link: attrs&fileAttributeReparsePoint != 0})
			}
			next := int(le.Uint32(e[0:]))
			if next == 0 {
				break
			}
			off += next
		}
	}
}
