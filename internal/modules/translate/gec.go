package translate

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"
	"strings"
	"time"
)

// winEpoch is the seconds between 1601, where Windows file times start, and
// 1970.
const winEpoch = 11644473600

// secMSGEC is the Sec-MS-GEC token the read aloud service wants with every
// connection. It proves the client knows the trusted client token, and it
// only holds for the 5-minute slot of the clock it was made in, so a wrong
// system clock gets the connection refused. It is "<16 hex>-<64 hex>": a
// checksum and a SHA-256 of the same text. Both are the algorithm of Edge as
// the open-source edge-tts project reproduces it (its drm.py, "v2").
func secMSGEC(now time.Time) string {
	// File time ticks (100 ns), rounded down to 5 minutes.
	ticks := (now.Unix() + winEpoch) / 300 * 300 * 10_000_000
	data := []byte(fmt.Sprintf("%d%s", ticks, edgeClientToken))
	sum := saltedSHA256(data)
	a, b := cs64(data, sum)
	var check [8]byte
	binary.LittleEndian.PutUint32(check[:4], a)
	binary.LittleEndian.PutUint32(check[4:], b)
	return strings.ToUpper(hex.EncodeToString(check[:]) + "-" + hex.EncodeToString(sum[:]))
}

// The SHA-256 of the token is taken over a 57-byte salt followed by the
// data. The salt is not in the source: the state after the first 14 rounds
// of the first block, and what the salt adds to the next message words, are
// kept as constants instead.
var (
	sha256Initial = [8]uint32{0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A, 0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19}
	// saltedState is the working state after round 14 of the first block.
	saltedState = [8]uint32{0xFD9FBEA2, 0x91243CE7, 0xBE499094, 0xCD2E1E0A, 0x6C006F49, 0x04BD35AB, 0xFEB0A7E4, 0x03EFFBD2}
	// saltedSchedule is the salt's part of message words 16 to 29.
	saltedSchedule = [14]uint32{
		0xEB24BF9E, 0x1558691F, 0xA0C09EE2, 0x0CFA32BA, 0xA622EE8C, 0xD1DD9491, 0xC0DD25A0,
		0xF9E70AB6, 0xF0C31573, 0x7058CE4E, 0x7188E606, 0x00E5AB33, 0xD258B060, 0x72766564,
	}
	sha256K = [64]uint32{
		0x428A2F98, 0x71374491, 0xB5C0FBCF, 0xE9B5DBA5, 0x3956C25B, 0x59F111F1, 0x923F82A4, 0xAB1C5ED5,
		0xD807AA98, 0x12835B01, 0x243185BE, 0x550C7DC3, 0x72BE5D74, 0x80DEB1FE, 0x9BDC06A7, 0xC19BF174,
		0xE49B69C1, 0xEFBE4786, 0x0FC19DC6, 0x240CA1CC, 0x2DE92C6F, 0x4A7484AA, 0x5CB0A9DC, 0x76F988DA,
		0x983E5152, 0xA831C66D, 0xB00327C8, 0xBF597FC7, 0xC6E00BF3, 0xD5A79147, 0x06CA6351, 0x14292967,
		0x27B70A85, 0x2E1B2138, 0x4D2C6DFC, 0x53380D13, 0x650A7354, 0x766A0ABB, 0x81C2C92E, 0x92722C85,
		0xA2BFE8A1, 0xA81A664B, 0xC24B8B70, 0xC76C51A3, 0xD192E819, 0xD6990624, 0xF40E3585, 0x106AA070,
		0x19A4C116, 0x1E376C08, 0x2748774C, 0x34B0BCB5, 0x391C0CB3, 0x4ED8AA4A, 0x5B9CCA4F, 0x682E6FF3,
		0x748F82EE, 0x78A5636F, 0x84C87814, 0x8CC70208, 0x90BEFFFA, 0xA4506CEB, 0xBEF9A3F7, 0xC67178F2,
	}
)

const (
	// saltedConstant replaces round constant 14 in the first block, and
	// stands for the salt in message word 30.
	saltedConstant = 0xC9DC06A7
	// The salt also changes the first and last of the precomputed words.
	sigma1Adjustment = 0x000B92C0
	sigma0Adjustment = 0x059C0B80
	// saltLength is the length of the salt in bytes.
	saltLength = 57
)

func sigma0(x uint32) uint32 {
	return bits.RotateLeft32(x, -7) ^ bits.RotateLeft32(x, -18) ^ x>>3
}

func sigma1(x uint32) uint32 {
	return bits.RotateLeft32(x, -17) ^ bits.RotateLeft32(x, -19) ^ x>>10
}

// expandSchedule fills the message words from index first on.
func expandSchedule(w *[64]uint32, first int) {
	for i := first; i < 64; i++ {
		w[i] = sigma1(w[i-2]) + w[i-7] + sigma0(w[i-15]) + w[i-16]
	}
}

// compress runs SHA-256 rounds from round first on over the words, from the
// given state, with round constants k, and returns the working state.
func compress(state [8]uint32, w *[64]uint32, first int, k *[64]uint32) [8]uint32 {
	a, b, c, d, e, f, g, h := state[0], state[1], state[2], state[3], state[4], state[5], state[6], state[7]
	for i := first; i < 64; i++ {
		t1 := h + (bits.RotateLeft32(e, -6) ^ bits.RotateLeft32(e, -11) ^ bits.RotateLeft32(e, -25)) + (g ^ (e & (f ^ g))) + k[i] + w[i]
		t2 := (bits.RotateLeft32(a, -2) ^ bits.RotateLeft32(a, -13) ^ bits.RotateLeft32(a, -22)) + ((a & b) | (c & (a | b)))
		a, b, c, d, e, f, g, h = t1+t2, a, b, c, d+t1, e, f, g
	}
	return [8]uint32{a, b, c, d, e, f, g, h}
}

// saltedSHA256 is the SHA-256 of the salt followed by data, which must be
// at least 7 bytes. The first block holds the salt and the first 7 bytes of
// data; its state is rebuilt from the constants.
func saltedSHA256(data []byte) [32]byte {
	var w [64]uint32
	w[14] = uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2])
	w[15] = binary.BigEndian.Uint32(data[3:7])
	for i := 16; i < 30; i++ {
		v := saltedSchedule[i-16]
		if i == 16 {
			v += sigma1(w[14]) ^ sigma1Adjustment
		} else {
			v += sigma1(w[i-2])
		}
		if i >= 21 {
			v += w[i-7]
		}
		if i == 29 {
			v += sigma0(w[14]) ^ sigma0Adjustment
		}
		w[i] = v
	}
	w[30] = sigma1(w[28]) + w[23] + sigma0(w[15]) + w[14] + saltedConstant - sha256K[14]
	expandSchedule(&w, 31)
	k := sha256K
	k[14] = saltedConstant
	working := compress(saltedState, &w, 14, &k)
	var state [8]uint32
	for i := range state {
		state[i] = sha256Initial[i] + working[i]
	}

	// The rest of the data is hashed as usual, as if after the salt.
	length := saltLength + len(data)
	padded := append([]byte{}, data[7:]...)
	padded = append(padded, 0x80)
	for i := ((55-length)%64 + 64) % 64; i > 0; i-- {
		padded = append(padded, 0)
	}
	padded = binary.BigEndian.AppendUint64(padded, uint64(length)*8)
	for off := 0; off < len(padded); off += 64 {
		var block [64]uint32
		for i := 0; i < 16; i++ {
			block[i] = binary.BigEndian.Uint32(padded[off+i*4:])
		}
		expandSchedule(&block, 16)
		working := compress(state, &block, 0, &sha256K)
		for i := range state {
			state[i] += working[i]
		}
	}
	var out [32]byte
	for i, v := range state {
		binary.BigEndian.PutUint32(out[i*4:], v)
	}
	return out
}

var cs64Coefficients = [14][2]uint32{
	{0xE82E91B1, 0xB32700D0},
	{0x44DE24C6, 0x88DFF51D},
	{0x3BEB65BC, 0x29514038},
	{0xC89C5933, 0xB328CBDF},
	{0x8D1EC211, 0xD405B4D5},
	{0x314EF5D5, 0x26E55F3C},
	{0xE92491B1, 0x607801C6},
	{0xF0A5B9D5, 0x82B2C1FB},
	{0x06064CF3, 0xFA51A3DA},
	{0xDA79FA60, 0x207CC464},
	{0xBED20560, 0x3C3C74F3},
	{0x89F9C9F8, 0xD2D14B61},
	{0x35FA38C6, 0xBB04A2C1},
	{0xCB49E3C6, 0xC5626167},
}

// rol16 rotates a 32-bit value left by 16 bits.
func rol16(v uint32) uint32 {
	return v<<16 | v>>16
}

// cs64 is the first half of the token: a checksum of data keyed by the
// first bytes of its SHA-256 sum. All arithmetic wraps at 32 bits.
func cs64(data []byte, sum [32]byte) (uint32, uint32) {
	keyA := binary.LittleEndian.Uint32(sum[0:4]) | 1
	keyB := binary.LittleEndian.Uint32(sum[4:8]) | 1
	key := [2]uint32{keyA, keyB}
	const factor = 0x82893933
	factors := [2]uint32{keyA * factor, keyB * factor}

	terms := make([]uint32, 0, len(cs64Coefficients)+len(data)/4+3)
	offsets := make([]uint32, 0, cap(terms))
	for i, c := range cs64Coefficients {
		terms = append(terms, c[0]*factors[i%2])
		offsets = append(offsets, c[1])
	}

	// The data is read as little-endian words after a zero byte, padded with
	// zeros to an even number of words.
	padded := append([]byte{0}, data...)
	for len(padded)%8 != 0 {
		padded = append(padded, 0)
	}
	multipliers := [2]uint32{0xEF0569FB, 0xC31713DB}
	for i := 0; i < len(padded)/4; i++ {
		word := binary.LittleEndian.Uint32(padded[i*4:])
		term := word * key[i%2]
		offset := rol16(word) * multipliers[i%2]
		if i == 0 {
			term += 0xF2F90B1A * factors[0]
			offset += 0x0B1A0000
		}
		terms = append(terms, term)
		offsets = append(offsets, offset)
	}

	var swapAcc, swapLast, state, acc uint32
	for i := 0; i < len(terms); i += 2 {
		x, y := terms[i], terms[i+1]
		q := rol16(offsets[i]+x) * 0x76F50F34
		swapAcc += rol16(q)*0xC3EFEA97 + q
		q = rol16(offsets[i+1]+y) * 0x1881CE3C
		swapLast = rol16(q)*0x35BD1EC9 + q
		swapAcc += swapLast

		state = state*keyA + x
		state = rol16(state) * 0xCF98B111
		state = rol16(state) * 0x87085B9F
		state = rol16(state) * 0x12CEB96D
		state = rol16(state) * 0x257E1D83
		acc += state
		t := state*keyB + y
		t = rol16(t) * 0xA27416F5
		t = rol16(t) * 0xD38396FF
		state = rol16(rol16(t)*0x7C932B89) * 0xBFA49F69
		acc += state
	}
	return swapLast ^ state, swapAcc ^ acc
}
