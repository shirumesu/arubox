// Package fileindex finds files by name the way Everything does: a service
// with administrator rights reads each NTFS volume's file table and follows
// its change journal, and the program keeps the names in memory and
// searches them in a few milliseconds.
package fileindex

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// rootRecord is the MFT record of a volume's root folder.
const rootRecord = 5

// volume is the index of one volume, kept as parallel arrays with no
// pointer per file, so a million files cost the garbage collector nothing
// to scan and take about 60 bytes each.
type volume struct {
	mu sync.RWMutex

	guid      string // the volume GUID path, \\?\Volume{…}\
	mount     string // where it is mounted, such as C:\
	journalID uint64
	nextUSN   int64

	// names holds each entry's name in UTF-8 followed by a 0, and folded
	// the same names lowercased; lowercasing keeps every character's
	// length, so both share off.
	names  []byte
	folded []byte
	off    []uint32
	parent []uint32 // the record number of the entry's folder
	rec    []uint32 // the entry's own record number
	seq    []uint16 // the record's sequence number
	flags  []byte

	// byRec maps a record number to its first entry plus one; 0 is none.
	// links holds the further entries of a file with several hard links.
	byRec []uint32
	links map[uint32][]uint32
	dead  int
}

func newVolume(guid, mount string, journalID uint64) *volume {
	return &volume{guid: guid, mount: mount, journalID: journalID, links: map[uint32][]uint32{}}
}

// link is one of a file's names: the folder it is in and its name there.
type link struct {
	parent uint32
	name   string
}

func (v *volume) count() int { return len(v.off) - v.dead }

// name returns entry i's name, folded or not.
func (v *volume) name(i int, folded bool) []byte {
	pool := v.names
	if folded {
		pool = v.folded
	}
	end := len(pool) - 1
	if i+1 < len(v.off) {
		end = int(v.off[i+1]) - 1
	}
	return pool[v.off[i]:end]
}

// add appends an entry and returns its index.
func (v *volume) add(rec uint32, seq uint16, parent uint32, flags byte, name string) int {
	i := len(v.off)
	v.off = append(v.off, uint32(len(v.names)))
	v.names = append(append(v.names, name...), 0)
	v.folded = appendFolded(v.folded, name)
	v.folded = append(v.folded, 0)
	v.parent = append(v.parent, parent)
	v.rec = append(v.rec, rec)
	v.seq = append(v.seq, seq)
	v.flags = append(v.flags, flags)
	return i
}

// index records entry i under its record number.
func (v *volume) index(i int) {
	r := v.rec[i]
	if int(r) >= len(v.byRec) {
		grown := make([]uint32, max(int(r)+1, len(v.byRec)*5/4))
		copy(grown, v.byRec)
		v.byRec = grown
	}
	if v.byRec[r] == 0 {
		v.byRec[r] = uint32(i) + 1
	} else {
		v.links[r] = append(v.links[r], uint32(i))
	}
}

// lookup returns the first entry of a record, or -1.
func (v *volume) lookup(rec uint32) int {
	if int(rec) >= len(v.byRec) {
		return -1
	}
	return int(v.byRec[rec]) - 1
}

// remove marks every entry of a record dead. A sequence number other than
// 0 must match, so a late delete of an earlier file in a reused record is
// ignored.
func (v *volume) remove(rec uint32, seq uint16) {
	first := v.lookup(rec)
	if first < 0 || (seq != 0 && v.seq[first] != seq) {
		return
	}
	for _, i := range append([]uint32{uint32(first)}, v.links[rec]...) {
		v.flags[i] |= flagDead
		v.dead++
	}
	v.byRec[rec] = 0
	delete(v.links, rec)
}

// set replaces a record's entries with its current names. A name is
// indexed only when its folder is: not excluded, nor inside an excluded
// folder.
func (v *volume) set(rec uint32, seq uint16, flags byte, links []link, ex *excludes) {
	v.remove(rec, 0)
	for _, l := range links {
		if l.parent != rootRecord {
			p := v.lookup(l.parent)
			if p < 0 || v.flags[p]&flagDir == 0 || v.flags[p]&flagExcluded != 0 {
				continue
			}
		}
		i := v.add(rec, seq, l.parent, flags, l.name)
		v.index(i)
		if flags&flagDir != 0 && ex.match(v, i) {
			v.flags[i] |= flagExcluded
		}
	}
	if v.dead > 4096 && v.dead > len(v.off)/4 {
		v.compact(ex)
	}
}

// Entry states while compacting.
const (
	stateUnknown byte = iota
	stateKeep
	// stateKeepEmpty keeps an excluded folder but drops what is inside.
	stateKeepEmpty
	stateDrop
)

// maxDepth bounds a walk up the folders, against a damaged file table
// whose folders loop.
const maxDepth = 512

// compact drops dead entries and everything that cannot be reached from
// the root or lies inside an excluded folder, and rebuilds the lookup by
// record. It also finishes a full listing, which arrives in file table
// order with children before their folders.
func (v *volume) compact(ex *excludes) {
	v.byRec, v.links = nil, map[uint32][]uint32{}
	for i := range v.off {
		if v.flags[i]&flagDead == 0 {
			v.index(i)
		}
	}
	n := len(v.off)
	state := make([]byte, n)
	var chain []int
	for i := range n {
		if state[i] != stateUnknown {
			continue
		}
		if v.flags[i]&flagDead != 0 {
			state[i] = stateDrop
			continue
		}
		// Walk up to an entry already decided, or the root, then decide
		// the walked entries from the top down.
		chain = chain[:0]
		above := stateDrop // the state of the folder above chain's top
		for j := i; ; {
			chain = append(chain, j)
			p := v.parent[j]
			if p == rootRecord {
				above = stateKeep
				break
			}
			k := v.lookup(p)
			if k < 0 || v.flags[k]&flagDir == 0 || len(chain) > maxDepth {
				break
			}
			if state[k] != stateUnknown {
				above = state[k]
				break
			}
			j = k
		}
		for c := len(chain) - 1; c >= 0; c-- {
			e := chain[c]
			switch {
			case above != stateKeep:
				state[e] = stateDrop
			case v.flags[e]&flagDir != 0 && ex.match(v, e):
				state[e] = stateKeepEmpty
			default:
				state[e] = stateKeep
			}
			above = state[e]
		}
	}

	kept := 0
	for _, s := range state {
		if s != stateDrop {
			kept++
		}
	}
	out := &volume{guid: v.guid, mount: v.mount, journalID: v.journalID, nextUSN: v.nextUSN, links: map[uint32][]uint32{}}
	out.off = make([]uint32, 0, kept)
	out.parent = make([]uint32, 0, kept)
	out.rec = make([]uint32, 0, kept)
	out.seq = make([]uint16, 0, kept)
	out.flags = make([]byte, 0, kept)
	out.names = make([]byte, 0, len(v.names))
	out.folded = make([]byte, 0, len(v.folded))
	for i, s := range state {
		if s == stateDrop {
			continue
		}
		flags := v.flags[i] &^ flagExcluded
		if s == stateKeepEmpty {
			flags |= flagExcluded
		}
		out.off = append(out.off, uint32(len(out.names)))
		out.names = append(append(out.names, v.name(i, false)...), 0)
		out.folded = append(append(out.folded, v.name(i, true)...), 0)
		out.parent = append(out.parent, v.parent[i])
		out.rec = append(out.rec, v.rec[i])
		out.seq = append(out.seq, v.seq[i])
		out.flags = append(out.flags, flags)
	}
	for i := range out.off {
		out.index(i)
	}
	v.names, v.folded, v.off, v.parent, v.rec, v.seq, v.flags = out.names, out.folded, out.off, out.parent, out.rec, out.seq, out.flags
	v.byRec, v.links, v.dead = out.byRec, out.links, 0
}

// path returns entry i's full path, folded or not. ok is false when the entry can no longer be reached, such as a
// file whose folder moved into an excluded folder.
func (v *volume) path(i int, folded bool) (path string, ok bool) {
	parts := [][]byte{v.name(i, folded)}
	for p := v.parent[i]; p != rootRecord; {
		k := v.lookup(p)
		if k < 0 || v.flags[k]&(flagDead|flagExcluded) != 0 || len(parts) > maxDepth {
			return "", false
		}
		parts = append(parts, v.name(k, folded))
		p = v.parent[k]
	}
	var b strings.Builder
	mount := v.mount
	if folded {
		mount = string(appendFolded(nil, mount))
	}
	b.WriteString(mount)
	for k := len(parts) - 1; k >= 0; k-- {
		b.Write(parts[k])
		if k > 0 {
			b.WriteByte('\\')
		}
	}
	return b.String(), true
}

// depth returns how many folders down entry i is, and whether it can be
// reached, like path but without building the path.
func (v *volume) depth(i int) (int, bool) {
	d := 1
	for p := v.parent[i]; p != rootRecord; d++ {
		k := v.lookup(p)
		if k < 0 || v.flags[k]&(flagDead|flagExcluded) != 0 || d > maxDepth {
			return 0, false
		}
		p = v.parent[k]
	}
	return d, true
}

// appendFolded appends s lowercased. A character whose lowercase form has
// another length in UTF-8 stays as it is, so folded names line up with
// the originals.
func appendFolded(b []byte, s string) []byte {
	for _, r := range s {
		if r < utf8.RuneSelf {
			if 'A' <= r && r <= 'Z' {
				r += 'a' - 'A'
			}
			b = append(b, byte(r))
			continue
		}
		if l := unicode.ToLower(r); utf8.RuneLen(l) == utf8.RuneLen(r) {
			r = l
		}
		b = utf8.AppendRune(b, r)
	}
	return b
}

// excludes are the folders the user keeps out of the index: by full path
// when the entry has a backslash, otherwise every folder of that name.
type excludes struct {
	names map[string]bool
	paths map[string]bool
	// last holds the last part of each path, so most folders are ruled
	// out without building their path.
	last map[string]bool
}

func newExcludes(list []string) *excludes {
	ex := &excludes{names: map[string]bool{}, paths: map[string]bool{}, last: map[string]bool{}}
	for _, e := range list {
		e = strings.TrimRight(strings.TrimSpace(strings.ReplaceAll(e, "/", `\`)), `\`)
		if e == "" {
			continue
		}
		f := string(appendFolded(nil, e))
		if strings.Contains(f, `\`) {
			ex.paths[f] = true
			ex.last[f[strings.LastIndexByte(f, '\\')+1:]] = true
		} else {
			ex.names[f] = true
		}
	}
	return ex
}

// match reports whether folder i is excluded.
func (ex *excludes) match(v *volume, i int) bool {
	name := v.name(i, true)
	if ex.names[string(name)] {
		return true
	}
	if !ex.last[string(name)] {
		return false
	}
	p, ok := v.path(i, true)
	return ok && ex.paths[p]
}
