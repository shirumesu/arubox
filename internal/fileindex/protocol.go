//go:build windows

package fileindex

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
)

// The index service and the program talk over a named pipe in frames: a
// little-endian uint32 payload length, a type byte and the payload.
//
// The program opens with a hello: the protocol version and, for each volume
// it has a saved index of, where the saved index left the volume's change
// journal. The service answers with its own version and the volumes it
// indexes, then streams, for each volume, either a full listing of the
// volume's files or nothing (when the saved index can catch up), followed by
// the volume's changes for as long as the program stays connected.

// protocolVersion changes whenever the frames change. A program and a
// service of different versions cannot talk, and the program asks to
// reinstall the service.
const protocolVersion = 1

// helloMagic starts the program's hello, so a stray client is told apart.
const helloMagic = "ARUBOXIX"

// Frame types sent by the service.
const (
	// frameVolume starts a volume: its index in this connection, volume
	// GUID path, mount path, journal ID and whether a full listing follows.
	frameVolume byte = iota + 1
	// frameRecords is a batch of a volume's full listing.
	frameRecords
	// frameScanned ends a volume's full listing.
	frameScanned
	// frameChanges is a batch of a volume's changes and the journal
	// position after them.
	frameChanges
	// frameReset reports why a volume's indexing failed; a frameVolume
	// with a full listing comes next.
	frameReset
)

// Change kinds inside a frameChanges.
const (
	changeDelete byte = iota
	changeSet
)

// Entry flags, as sent and as stored.
const (
	flagDir byte = 1 << iota
	flagHidden
	flagSystem
	// flagExcluded marks a folder the user excluded: it is kept, so
	// changes inside it are recognised and dropped, but nothing inside it
	// is indexed.
	flagExcluded
	// flagDead marks a deleted entry until the index is compacted.
	flagDead
	// flagCased marks, in the index only, an entry whose name has
	// capitals.
	flagCased
)

// maxFrame bounds a frame, so a corrupt length cannot make a reader
// allocate without limit.
const maxFrame = 16 << 20

// recordNumber is the MFT record number of a file reference: its low 48
// bits. The high 16 are the record's sequence number.
func recordNumber(frn uint64) uint32 { return uint32(frn & (1<<48 - 1)) }

func sequence(frn uint64) uint16 { return uint16(frn >> 48) }

// volumeState is where a saved index of a volume left its change journal.
type volumeState struct {
	GUID      string
	JournalID uint64
	NextUSN   int64
}

// encoder builds a frame's payload.
type encoder struct{ b []byte }

func (e *encoder) u8(v byte)    { e.b = append(e.b, v) }
func (e *encoder) u16(v uint16) { e.b = binary.LittleEndian.AppendUint16(e.b, v) }
func (e *encoder) u32(v uint32) { e.b = binary.LittleEndian.AppendUint32(e.b, v) }
func (e *encoder) u64(v uint64) { e.b = binary.LittleEndian.AppendUint64(e.b, v) }
func (e *encoder) str(s string) {
	e.u16(uint16(len(s)))
	e.b = append(e.b, s...)
}

// decoder reads a frame's payload. A read past the end sets err and
// returns zeros, so a frame is checked once after decoding it.
type decoder struct {
	b   []byte
	err error
}

var errShortFrame = errors.New("fileindex: frame too short")

func (d *decoder) take(n int) []byte {
	if d.err != nil || len(d.b) < n {
		d.err = errShortFrame
		return make([]byte, n)
	}
	v := d.b[:n]
	d.b = d.b[n:]
	return v
}

func (d *decoder) u8() byte      { return d.take(1)[0] }
func (d *decoder) u16() uint16   { return binary.LittleEndian.Uint16(d.take(2)) }
func (d *decoder) u32() uint32   { return binary.LittleEndian.Uint32(d.take(4)) }
func (d *decoder) u64() uint64   { return binary.LittleEndian.Uint64(d.take(8)) }
func (d *decoder) bytes() []byte { return d.take(int(d.u16())) }
func (d *decoder) str() string   { return string(d.bytes()) }

// writeFrame writes one frame.
func writeFrame(w io.Writer, kind byte, payload []byte) error {
	head := make([]byte, 5, 5+len(payload))
	binary.LittleEndian.PutUint32(head, uint32(len(payload)))
	head[4] = kind
	_, err := w.Write(append(head, payload...))
	return err
}

// readFrame reads one frame. The payload is valid until the next call.
func readFrame(r *bufio.Reader, buf []byte) (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(head[:4])
	if n > maxFrame {
		return 0, nil, errors.New("fileindex: frame too large")
	}
	if cap(buf) < int(n) {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return head[4], buf, nil
}

// encodeHello is the program's opening frame.
func encodeHello(states []volumeState) []byte {
	var e encoder
	e.b = append(e.b, helloMagic...)
	e.u32(protocolVersion)
	e.u16(uint16(len(states)))
	for _, s := range states {
		e.str(s.GUID)
		e.u64(s.JournalID)
		e.u64(uint64(s.NextUSN))
	}
	return e.b
}

func decodeHello(b []byte) (version uint32, states []volumeState, err error) {
	d := decoder{b: b}
	if string(d.take(len(helloMagic))) != helloMagic {
		return 0, nil, errors.New("fileindex: not a hello")
	}
	version = d.u32()
	if version != protocolVersion {
		return version, nil, d.err
	}
	for n := d.u16(); n > 0 && d.err == nil; n-- {
		states = append(states, volumeState{GUID: d.str(), JournalID: d.u64(), NextUSN: int64(d.u64())})
	}
	return version, states, d.err
}

// encodeWelcome is the service's answer to a hello: its version and the
// GUID paths of the volumes it indexes.
func encodeWelcome(guids []string) []byte {
	var e encoder
	e.u32(protocolVersion)
	e.u16(uint16(len(guids)))
	for _, g := range guids {
		e.str(g)
	}
	return e.b
}

func decodeWelcome(b []byte) (version uint32, guids []string, err error) {
	d := decoder{b: b}
	version = d.u32()
	if version != protocolVersion {
		return version, nil, d.err
	}
	for n := d.u16(); n > 0 && d.err == nil; n-- {
		guids = append(guids, d.str())
	}
	return version, guids, d.err
}
