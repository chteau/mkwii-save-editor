package wiisave

// Reading / editing rksys.dat, the (decrypted) Mario Kart Wii save.
//
// Layout (fixed size 0x2BC000):
//   0x00000   "RKSD" + version "0006"
//   0x00008   License 0 (RKPD)      ┐
//   0x08CC8   License 1 (RKPD)      │ 4 licenses of 0x8CC0 bytes
//   0x11988   License 2 (RKPD)      │
//   0x1A648   License 3 (RKPD)      ┘
//   0x27FFC   CRC-32 (zlib/IEEE) big-endian of everything before [0:0x27FFC]
//   0x28000+  Ghost data (RKGD)
//
// Useful offsets INSIDE a license (relative to the start of the RKPD block):
//   0x00 magic "RKPD"
//   0x14 Mii name, UTF-16BE, 0x14 bytes (10 chars max)
//   0x30 .. 0x37  unlock bitfields (cups / characters / vehicles)
//   0xB0 u16  Versus Rating (VR)
//   0xB2 u16  Battle Rating (BR)
//   0xB4 u32  total race count

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"unicode/utf16"
)

// RksysSize is the fixed size of a rksys.dat file.
const RksysSize = 0x2BC000

const (
	checksumOff = 0x27FFC

	relName      = 0x14
	relNameLen   = 0x14
	relUnlocks   = 0x30
	relUnlockLen = 8
	relVR        = 0xB0
	relBR        = 0xB2
	relRaceCount = 0xB4

	rksdMagic = "RKSD"
	rkpdMagic = "RKPD"
)

// licenseBases: absolute offsets of the 4 license blocks.
var licenseBases = [4]int{0x08, 0x8CC8, 0x11988, 0x1A648}

// License is an editable view of one license.
type License struct {
	Index       int
	Used        bool
	Name        string
	VR          int
	BR          int
	RaceCount   int
	UnlockBytes [8]byte
	Cups        []CupRank
	Records     []TTRecord
}

// Rksys is a rksys.dat save loaded in memory.
type Rksys struct {
	data []byte
}

// ParseRksys validates and loads a rksys.dat.
func ParseRksys(data []byte) (*Rksys, error) {
	if len(data) != RksysSize {
		return nil, errors.New("unexpected size for a rksys.dat (expected 0x2BC000)")
	}
	if string(data[0:4]) != rksdMagic {
		return nil, errors.New("missing \"RKSD\" magic")
	}
	return &Rksys{data: data}, nil
}

// Bytes returns the current contents (after edits + checksum).
func (r *Rksys) Bytes() []byte { return r.data }

// Licenses reads the state of the 4 licenses.
func (r *Rksys) Licenses() []License {
	out := make([]License, 4)
	for i, base := range licenseBases {
		l := License{Index: i}
		l.Used = string(r.data[base:base+4]) == rkpdMagic
		if l.Used {
			l.Name = decodeName(r.data[base+relName : base+relName+relNameLen])
			l.VR = int(binary.BigEndian.Uint16(r.data[base+relVR:]))
			l.BR = int(binary.BigEndian.Uint16(r.data[base+relBR:]))
			l.RaceCount = int(binary.BigEndian.Uint32(r.data[base+relRaceCount:]))
			copy(l.UnlockBytes[:], r.data[base+relUnlocks:base+relUnlocks+relUnlockLen])
			l.Cups = r.readCups(base)
			l.Records = r.readRecords(base, i)
		}
		out[i] = l
	}
	return out
}

// LicenseEdit describes the fields to apply to a license.
type LicenseEdit struct {
	Index     int
	Name      string
	VR        int
	BR        int
	RaceCount int
	Unlocks   []int // 8 bytes 0-255
	Cups      []CupRankEdit
	Records   []TTRecordEdit
}

// Apply writes an edit into the buffer (without recomputing the checksum).
func (r *Rksys) Apply(e LicenseEdit) error {
	if e.Index < 0 || e.Index >= 4 {
		return errors.New("invalid license index")
	}
	base := licenseBases[e.Index]
	if string(r.data[base:base+4]) != rkpdMagic {
		return errors.New("empty license: cannot edit it")
	}

	copy(r.data[base+relName:base+relName+relNameLen], encodeName(e.Name))
	binary.BigEndian.PutUint16(r.data[base+relVR:], uint16(clamp(e.VR, 0, 0xFFFF)))
	binary.BigEndian.PutUint16(r.data[base+relBR:], uint16(clamp(e.BR, 0, 0xFFFF)))
	binary.BigEndian.PutUint32(r.data[base+relRaceCount:], uint32(e.RaceCount))

	if len(e.Unlocks) == relUnlockLen {
		for k, v := range e.Unlocks {
			r.data[base+relUnlocks+k] = byte(clamp(v, 0, 0xFF))
		}
	}
	for _, c := range e.Cups {
		r.applyCup(base, c)
	}
	for _, rec := range e.Records {
		r.applyRecord(base, e.Index, rec)
	}
	return nil
}

// FixChecksum recomputes and writes the CRC-32 at 0x27FFC. Call before saving.
func (r *Rksys) FixChecksum() {
	crc := crc32.ChecksumIEEE(r.data[:checksumOff])
	binary.BigEndian.PutUint32(r.data[checksumOff:], crc)
}

// ChecksumOK checks the current CRC-32 (diagnostic).
func (r *Rksys) ChecksumOK() bool {
	stored := binary.BigEndian.Uint32(r.data[checksumOff:])
	return stored == crc32.ChecksumIEEE(r.data[:checksumOff])
}

//== Mii name (UTF-16BE) & helpers

// decodeName converts UTF-16BE bytes to a string, stopping at the first null.
func decodeName(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.BigEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// encodeName converts a string to UTF-16BE over exactly relNameLen bytes
// (10 units), truncated if needed and zero-padded.
func encodeName(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, relNameLen)
	for i := 0; i < relNameLen/2 && i < len(u); i++ {
		binary.BigEndian.PutUint16(out[i*2:], u[i])
	}
	return out
}

// clamp returns v constrained to the inclusive range [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
