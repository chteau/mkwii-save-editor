package wiisave

// rksys.dat editor extensions:
//   - Cup star ranks ("Cup Data" section)
//   - Time Trial records ("Time Trial Leaderboards" section), with synchronization
//     of the matching RKG ghost (RKGD) + recomputation of its CRC.

import (
	"encoding/binary"
	"hash/crc32"
)

//== Cup data (star ranks)

const (
	cupBase   = 0x1C0 // first cup entry (relative to the license)
	cupStride = 0x60  // size of a cup entry
	cupCount  = 32    // 8 cups × 4 classes

	// Encoding determined empirically (the wiki was ambiguous / uses MSB numbering).
	// A NON-completed cup has its whole entry zeroed.
	relCupTrophy = 0x4F // 2 HIGH bits: 0=Gold 1=Silver 2=Bronze 3=none
	relCupRank   = 0x51 // LOW nibble: 0=3★ 1=2★ 2=1★ 3=A … 8=F
	relCupDone   = 0x52 // bit 7 (0x80): cup completed

	rankNone = 15 // sentinel value "—" (not completed) for the UI
)

// CupNames are the 8 cups in internal order (a cup index is cc*8 + cup).
var CupNames = []string{"Mushroom", "Flower", "Star", "Special", "Shell", "Banana", "Leaf", "Lightning"}

// CCNames are the 4 engine classes in internal order.
var CCNames = []string{"50cc", "100cc", "150cc", "Mirror"}

// CupRank holds the state of one cup in a given engine class.
type CupRank struct {
	Index  int    // 0..31
	Label  string // e.g. "150cc Star"
	Trophy int    // 0=Gold 1=Silver 2=Bronze 3=none
	Rank   int    // 0=3★ … 8=F, 15=unset
}

// CupRankEdit is an edit applied to one cup.
type CupRankEdit struct {
	Index  int
	Trophy int
	Rank   int
}

// readCups reads the 32 cup star ranks of the license at the given base offset.
func (r *Rksys) readCups(base int) []CupRank {
	out := make([]CupRank, cupCount)
	for i := 0; i < cupCount; i++ {
		off := base + cupBase + i*cupStride
		cc, cup := i/8, i%8
		c := CupRank{Index: i, Label: CCNames[cc] + " " + CupNames[cup], Trophy: 3, Rank: rankNone}
		if r.data[off+relCupDone]&0x80 != 0 { // cup completed
			c.Trophy = int(r.data[off+relCupTrophy]>>6) & 0x03
			c.Rank = int(r.data[off+relCupRank]) & 0x0F
		}
		out[i] = c
	}
	return out
}

// applyCup writes one cup's rank/trophy (or clears it when the rank is "none").
func (r *Rksys) applyCup(base int, e CupRankEdit) {
	if e.Index < 0 || e.Index >= cupCount {
		return
	}
	off := base + cupBase + e.Index*cupStride

	if e.Rank < 0 || e.Rank > 8 { // "—": mark as not completed
		r.data[off+relCupDone] &^= 0x80
		return
	}
	// Completed cup: set the completion bit, write trophy and rank while preserving
	// the other bits (character/vehicle metadata).
	r.data[off+relCupDone] |= 0x80
	trophy := e.Trophy
	if trophy < 0 || trophy > 2 { // "—" while completing -> default Gold
		trophy = 0
	}
	r.data[off+relCupTrophy] = (r.data[off+relCupTrophy] & 0x3F) | byte(trophy<<6)
	r.data[off+relCupRank] = (r.data[off+relCupRank] & 0xF0) | byte(e.Rank&0x0F)
}

//== Time Trial records + RKG ghosts

const (
	ttRank1    = 0xDC0 // "rank 1" leaderboard block (relative to the license)
	ttStride   = 0x60  // size of an entry (1 per track)
	ttCount    = 32    // 32 tracks
	relTime    = 0x4C  // time packed 7/7/10 bits (3 bytes) — see packTime
	relEnabled = 0x50  // bit 7: entry filled in

	ghostBlockBase   = 0x28000 // ghost block of license 0 (absolute in the file)
	ghostBlockStride = 0xA5000 // stride between licenses
	pbGhostStride    = 0x2800  // size of one PB ghost slot
	ghostTimeOff     = 0x04    // time in the RKG header (same 7/7/10 format)
	ghostCRCOff      = 0x27FC  // ghost CRC-32, over slot[0:0x27FC]
)

// TrackNames are the tracks in rksys array order (= cup order).
var TrackNames = []string{
	"Luigi Circuit", "Moo Moo Meadows", "Mushroom Gorge", "Toad's Factory",
	"Mario Circuit", "Coconut Mall", "DK Summit", "Wario's Gold Mine",
	"Daisy Circuit", "Koopa Cape", "Maple Treeway", "Grumble Volcano",
	"Dry Dry Ruins", "Moonview Highway", "Bowser's Castle", "Rainbow Road",
	"GCN Peach Beach", "DS Yoshi Falls", "SNES Ghost Valley 2", "N64 Mario Raceway",
	"N64 Sherbet Land", "GBA Shy Guy Beach", "DS Delfino Square", "GCN Waluigi Stadium",
	"DS Desert Hills", "GBA Bowser Castle 3", "N64 DK's Jungle Parkway", "GCN Mario Circuit",
	"SNES Mario Circuit 3", "DS Peach Gardens", "GCN DK Mountain", "N64 Bowser's Castle",
}

// TTRecord is a filled-in Time Trial record (rank 1).
type TTRecord struct {
	Slot     int
	Track    string
	Min      int
	Sec      int
	Ms       int
	HasGhost bool
}

// TTRecordEdit sets a new time for an existing record.
type TTRecordEdit struct {
	Slot int
	Min  int
	Sec  int
	Ms   int
}

// readRecords returns the filled-in (rank 1) Time Trial records of a license.
func (r *Rksys) readRecords(base, licIndex int) []TTRecord {
	var out []TTRecord
	for i := 0; i < ttCount; i++ {
		off := base + ttRank1 + i*ttStride
		enabled := r.data[off+relEnabled]&0x80 != 0
		if !enabled {
			continue
		}
		m, s, ms := unpackTime(r.data[off+relTime:])
		out = append(out, TTRecord{
			Slot:  i,
			Track: TrackNames[i],
			Min:   m, Sec: s, Ms: ms,
			HasGhost: r.ghostPresent(licIndex, i),
		})
	}
	return out
}

// applyRecord writes a record's time and syncs the matching ghost (time + CRC).
func (r *Rksys) applyRecord(base, licIndex int, e TTRecordEdit) {
	if e.Slot < 0 || e.Slot >= ttCount {
		return
	}
	off := base + ttRank1 + e.Slot*ttStride
	packTime(r.data[off+relTime:], e.Min, e.Sec, e.Ms)

	// Sync the matching PB ghost if present (otherwise the menu and the ghost would
	// show different times).
	if r.ghostPresent(licIndex, e.Slot) {
		slot := ghostBlockBase + licIndex*ghostBlockStride + e.Slot*pbGhostStride
		packTime(r.data[slot+ghostTimeOff:], e.Min, e.Sec, e.Ms)
		// Recompute the ghost's internal CRC-32.
		crc := crc32.ChecksumIEEE(r.data[slot : slot+ghostCRCOff])
		binary.BigEndian.PutUint32(r.data[slot+ghostCRCOff:], crc)
	}
}

// ghostPresent reports whether a PB ghost (RKGD) exists for that license/track.
func (r *Rksys) ghostPresent(licIndex, slotIndex int) bool {
	slot := ghostBlockBase + licIndex*ghostBlockStride + slotIndex*pbGhostStride
	if slot+4 > len(r.data) {
		return false
	}
	return string(r.data[slot:slot+4]) == "RKGD"
}

// packTime writes min/sec/ms in 7-bit / 7-bit / 10-bit (24-bit) format over the
// first 3 bytes, preserving the 4th byte (vehicle / track ID depending on the case).
func packTime(b []byte, min, sec, ms int) {
	v := uint32(clamp(min, 0, 0x7F))<<17 | uint32(clamp(sec, 0, 0x7F))<<10 | uint32(clamp(ms, 0, 0x3FF))
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}

// unpackTime decodes the 7/7/10-bit packed time from the first 3 bytes.
func unpackTime(b []byte) (min, sec, ms int) {
	v := uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
	return int(v>>17) & 0x7F, int(v>>10) & 0x7F, int(v) & 0x3FF
}
