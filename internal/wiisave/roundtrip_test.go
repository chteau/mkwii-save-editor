package wiisave

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"testing"
)

const realBin = "/home/cheeteau/Documents/Dolphin/private/wii/title/RMCP/data.bin"

func loadRealBin(t *testing.T) ([]byte, *DataBin) {
	t.Helper()
	raw, err := os.ReadFile(realBin)
	if err != nil {
		t.Skipf("real data.bin missing: %v", err)
	}
	db, err := ParseDataBin(raw)
	if err != nil {
		t.Fatalf("ParseDataBin: %v", err)
	}
	return raw, db
}

// A no-edit round-trip must be byte-for-byte identical to the original.
func TestRebuildIdentity(t *testing.T) {
	raw, db := loadRealBin(t)
	out, err := db.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !bytes.Equal(raw, out) {
		t.Fatalf("round-trip not identical (len %d vs %d)", len(raw), len(out))
	}
}

// The inner rksys.dat parses and the original CRC is valid.
func TestInnerRksysValid(t *testing.T) {
	_, db := loadRealBin(t)
	f := db.FindFile("rksys.dat")
	if f == nil {
		t.Fatal("rksys.dat not found")
	}
	rk, err := ParseRksys(f.Plain[:RksysSize])
	if err != nil {
		t.Fatalf("ParseRksys: %v", err)
	}
	if !rk.ChecksumOK() {
		t.Fatal("original CRC invalid")
	}
	lics := rk.Licenses()
	if !lics[0].Used {
		t.Fatal("expected license 0 to be used")
	}
	t.Logf("License 0: name=%q VR=%d BR=%d races=%d unlocks=% x",
		lics[0].Name, lics[0].VR, lics[0].BR, lics[0].RaceCount, lics[0].UnlockBytes)
}

// Full edit: name, VR/BR, unlocks -> recomputed CRC -> consistent re-read.
func TestEditAndChecksum(t *testing.T) {
	_, db := loadRealBin(t)
	f := db.FindFile("rksys.dat")
	rk, _ := ParseRksys(f.Plain[:RksysSize])

	err := rk.Apply(LicenseEdit{
		Index: 0, Name: "EDIT", VR: 9999, BR: 1234, RaceCount: 4242,
		Unlocks: []int{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rk.FixChecksum()
	if !rk.ChecksumOK() {
		t.Fatal("CRC invalid after edit")
	}

	// Re-read from the produced bytes.
	rk2, err := ParseRksys(rk.Bytes())
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	l := rk2.Licenses()[0]
	if l.Name != "EDIT" || l.VR != 9999 || l.BR != 1234 || l.RaceCount != 4242 {
		t.Fatalf("inconsistent re-read: %+v", l)
	}
	for i, b := range l.UnlockBytes {
		if b != 0xff {
			t.Fatalf("unlock byte %d = %#x, expected 0xff", i, b)
		}
	}
	if !rk2.ChecksumOK() {
		t.Fatal("re-read CRC invalid")
	}
}

// Editing a cup rank and a TT time: consistent re-read, valid global CRC, and the
// synced ghost's internal CRC correctly recomputed.
func TestEditCupsAndRecords(t *testing.T) {
	_, db := loadRealBin(t)
	f := db.FindFile("rksys.dat")
	rk, _ := ParseRksys(f.Plain[:RksysSize])

	before := rk.Licenses()[0]
	if len(before.Records) == 0 {
		t.Skip("no TT record in the save to test ghost sync")
	}
	rec := before.Records[0] // first record (known slot)

	err := rk.Apply(LicenseEdit{
		Index:   0,
		Name:    before.Name,
		VR:      before.VR,
		BR:      before.BR,
		Cups:    []CupRankEdit{{Index: 0, Trophy: 2, Rank: 5}}, // 50cc Mushroom -> Bronze, rank C
		Records: []TTRecordEdit{{Slot: rec.Slot, Min: 0, Sec: 58, Ms: 123}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rk.FixChecksum()
	if !rk.ChecksumOK() {
		t.Fatal("global CRC invalid after edit")
	}

	rk2, _ := ParseRksys(rk.Bytes())
	l := rk2.Licenses()[0]
	if l.Cups[0].Trophy != 2 || l.Cups[0].Rank != 5 {
		t.Fatalf("cup 0 re-read: trophy=%d rank=%d (expected 2/5)", l.Cups[0].Trophy, l.Cups[0].Rank)
	}
	var got TTRecord
	for _, x := range l.Records {
		if x.Slot == rec.Slot {
			got = x
		}
	}
	if got.Min != 0 || got.Sec != 58 || got.Ms != 123 {
		t.Fatalf("record re-read: %d:%02d.%03d (expected 0:58.123)", got.Min, got.Sec, got.Ms)
	}

	// Check the ghost was synced + CRC valid.
	if got.HasGhost {
		data := rk2.Bytes()
		slot := ghostBlockBase + 0*ghostBlockStride + rec.Slot*pbGhostStride
		m, s, ms := unpackTime(data[slot+ghostTimeOff:])
		if m != 0 || s != 58 || ms != 123 {
			t.Fatalf("ghost time: %d:%02d.%03d (expected 0:58.123)", m, s, ms)
		}
		stored := binary.BigEndian.Uint32(data[slot+ghostCRCOff:])
		calc := crc32.ChecksumIEEE(data[slot : slot+ghostCRCOff])
		if stored != calc {
			t.Fatalf("ghost CRC invalid: stored=%08x calc=%08x", stored, calc)
		}
		t.Logf("ghost slot %d (%s) synced + CRC %08x OK", rec.Slot, got.Track, calc)
	}
}
