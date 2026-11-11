//go:build windows

// Package fileindex finds files by name the way Everything does: a service
// with administrator rights reads each NTFS volume's file table and follows
// its change journal, and the program keeps the names in memory and
// searches them in a few milliseconds.
package fileindex

import (
	"bytes"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// rootRecord is the MFT record of a volume's root folder.
const rootRecord = 5

// volume is the index of one volume, kept as parallel arrays with no
// pointer per file, so a million files cost the garbage collector nothing
// to scan and take about 40 bytes each besides their names.
type volume struct {
	mu sync.RWMutex

	guid      string // the volume GUID path, \\?\Volume{…}\
	mount     string // where it is mounted, such as C:\
	journalID uint64
	nextUSN   int64

	// names holds each entry's name lowercased in UTF-8, followed by a 0,
	// which is what a search goes through.
	names  []byte
	off    []uint32
	parent []uint32 // the record number of the entry's folder
	rec    []uint32 // the entry's own record number
	seq    []uint16 // the record's sequence number
	flags  []byte

	// Most names have no capitals, so only the others are kept as written
	// as well: cased holds them, each followed by a 0, casedAt their
	// entries in order and casedOff where each starts. Their entries have
	// flagCased.
	cased    []byte
	casedAt  []uint32
	casedOff []uint32

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

// name returns entry i's name, folded or as written.
func (v *volume) name(i int, folded bool) []byte {
	if folded || v.flags[i]&flagCased == 0 {
		end := len(v.names) - 1
		if i+1 < len(v.off) {
			end = int(v.off[i+1]) - 1
		}
		return v.names[v.off[i]:end]
	}
	k, _ := slices.BinarySearch(v.casedAt, uint32(i))
	end := len(v.cased) - 1
	if k+1 < len(v.casedOff) {
		end = int(v.casedOff[k+1]) - 1
	}
	return v.cased[v.casedOff[k]:end]
}

// add appends an entry and returns its index.
func (v *volume) add(rec uint32, seq uint16, parent uint32, flags byte, name []byte) int {
	i := len(v.off)
	start := len(v.names)
	v.off = grow(v.off, 1)
	v.names = grow(v.names, len(name)+1)
	v.parent, v.rec, v.seq, v.flags = grow(v.parent, 1), grow(v.rec, 1), grow(v.seq, 1), grow(v.flags, 1)
	v.off = append(v.off, uint32(start))
	v.names = appendFolded(v.names, name)
	flags &^= flagCased
	if !bytes.Equal(v.names[start:], name) {
		flags |= flagCased
		v.addCased(i, name)
	}
	v.names = append(v.names, 0)
	v.parent = append(v.parent, parent)
	v.rec = append(v.rec, rec)
	v.seq = append(v.seq, seq)
	v.flags = append(v.flags, flags)
	return i
}

// addCased keeps entry i's name as written.
func (v *volume) addCased(i int, name []byte) {
	v.casedAt, v.casedOff = grow(v.casedAt, 1), grow(v.casedOff, 1)
	v.cased = grow(v.cased, len(name)+1)
	v.casedAt = append(v.casedAt, uint32(i))
	v.casedOff = append(v.casedOff, uint32(len(v.cased)))
	v.cased = append(append(v.cased, name...), 0)
}

// grow makes room for n more elements. The arrays are large and change
// little once listed, so they grow by a sixteenth rather than append's
// quarter, which would leave megabytes unused after the first change.
func grow[S ~[]E, E any](s S, n int) S {
	if cap(s)-len(s) >= n {
		return s
	}
	t := make(S, len(s), len(s)+max(n, len(s)/16, 1024))
	copy(t, s)
	return t
}

// reindex rebuilds the lookup by record from the live entries.
func (v *volume) reindex() {
	top := uint32(0)
	for i, r := range v.rec {
		if v.flags[i]&flagDead == 0 {
			top = max(top, r)
		}
	}
	v.byRec, v.links = make([]uint32, top+1), map[uint32][]uint32{}
	for i := range v.off {
		if v.flags[i]&flagDead == 0 {
			v.index(i)
		}
	}
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
		i := v.add(rec, seq, l.parent, flags, []byte(l.name))
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
	v.reindex()
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

	kept, keptNames, keptCased, keptCasedNames := 0, 0, 0, 0
	for i, s := range state {
		if s == stateDrop {
			continue
		}
		kept++
		keptNames += len(v.name(i, true)) + 1
		if v.flags[i]&flagCased != 0 {
			keptCased++
			keptCasedNames += len(v.name(i, false)) + 1
		}
	}
	out := &volume{}
	out.off = make([]uint32, 0, kept)
	out.parent = make([]uint32, 0, kept)
	out.rec = make([]uint32, 0, kept)
	out.seq = make([]uint16, 0, kept)
	out.flags = make([]byte, 0, kept)
	out.names = make([]byte, 0, keptNames)
	out.casedAt = make([]uint32, 0, keptCased)
	out.casedOff = make([]uint32, 0, keptCased)
	out.cased = make([]byte, 0, keptCasedNames)
	for i, s := range state {
		if s == stateDrop {
			continue
		}
		flags := v.flags[i] &^ flagExcluded
		if s == stateKeepEmpty {
			flags |= flagExcluded
		}
		if flags&flagCased != 0 {
			out.addCased(len(out.off), v.name(i, false))
		}
		out.off = append(out.off, uint32(len(out.names)))
		out.names = append(append(out.names, v.name(i, true)...), 0)
		out.parent = append(out.parent, v.parent[i])
		out.rec = append(out.rec, v.rec[i])
		out.seq = append(out.seq, v.seq[i])
		out.flags = append(out.flags, flags)
	}
	v.names, v.off, v.parent, v.rec, v.seq, v.flags = out.names, out.off, out.parent, out.rec, out.seq, out.flags
	v.cased, v.casedAt, v.casedOff = out.cased, out.casedAt, out.casedOff
	v.dead = 0
	v.reindex()
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
		mount = string(appendFolded(nil, []byte(mount)))
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
// another length in UTF-8 stays as it is, so a folded name is as long as
// the original.
func appendFolded(b, s []byte) []byte {
	for len(s) > 0 {
		c := s[0]
		if c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			b = append(b, c)
			s = s[1:]
			continue
		}
		r, n := utf8.DecodeRune(s)
		if l := unicode.ToLower(r); r != utf8.RuneError && utf8.RuneLen(l) == n {
			b = utf8.AppendRune(b, l)
		} else {
			b = append(b, s[:n]...)
		}
		s = s[n:]
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
		f := string(appendFolded(nil, []byte(e)))
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
