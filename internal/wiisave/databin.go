package wiisave

// Reading / rewriting the "data.bin" format: the Wii save as exported to an SD
// card (path private/wii/title/<ID>/data.bin). Layout:
//
//   0x0000                 Banner, 0xF0C0 bytes, AES-128-CBC encrypted (SDKey, SDIV)
//   0xF0C0                 "Bk" (backup) header, 0x80 bytes, PLAINTEXT
//   0xF140                 For each file:
//                              file header 0x80 bytes (PLAINTEXT)
//                              encrypted data (SDKey, IV taken from the header)
//                              padded to a multiple of 0x40
//   ...                    Footer (certificates + ECDSA signature)
//
// We never touch the banner, the headers or the footer: we only re-encrypt the
// inner files in place. Since the IV is zero and AES-CBC is deterministic,
// re-encoding an unchanged file yields the exact same bytes, so a no-edit
// round-trip is byte-for-byte identical.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	bannerSize  = 0xF0C0
	bkHeaderLen = 0x80
	fileHdrLen  = 0x80

	bkMagic   = "Bk"
	fileMagic = 0x03ADF17E
)

// File describes an inner file of the data.bin.
type File struct {
	Name       string
	Size       int    // logical size
	DataOffset int    // offset of the encrypted data in the raw .bin
	iv         []byte // IV (16 bytes) read from the file header
	Plain      []byte // decrypted contents (padded length)
}

// DataBin is an unpacked data.bin that keeps the original raw bytes so it can be
// rewritten identically, touching only the modified files.
type DataBin struct {
	raw   []byte // full copy of the original file (mutated on save)
	Files []*File
}

// ParseDataBin unpacks and decrypts a data.bin.
func ParseDataBin(raw []byte) (*DataBin, error) {
	if len(raw) < bannerSize+bkHeaderLen {
		return nil, errors.New("file too short to be a data.bin")
	}
	if string(raw[bannerSize+4:bannerSize+6]) != bkMagic {
		return nil, errors.New("\"Bk\" header not found: not a valid data.bin")
	}

	bk := raw[bannerSize : bannerSize+bkHeaderLen]
	nFiles := int(binary.BigEndian.Uint32(bk[0x0C:0x10]))

	db := &DataBin{raw: append([]byte(nil), raw...)}

	off := bannerSize + bkHeaderLen
	for i := 0; i < nFiles; i++ {
		if off+fileHdrLen > len(raw) {
			return nil, fmt.Errorf("header of file %d out of bounds", i)
		}
		hdr := raw[off : off+fileHdrLen]
		if binary.BigEndian.Uint32(hdr[0:4]) != fileMagic {
			return nil, fmt.Errorf("invalid file magic at offset %#x", off)
		}
		size := int(binary.BigEndian.Uint32(hdr[4:8]))
		name := cString(hdr[0x0B:0x4B])
		iv := append([]byte(nil), hdr[0x50:0x60]...)

		padded := (size + 0x3F) &^ 0x3F
		dataOff := off + fileHdrLen
		if dataOff+padded > len(raw) {
			return nil, fmt.Errorf("data of file %q out of bounds", name)
		}

		plain, err := aesCBCDecrypt(SDKey[:], iv, raw[dataOff:dataOff+padded])
		if err != nil {
			return nil, fmt.Errorf("decrypting %q: %w", name, err)
		}

		db.Files = append(db.Files, &File{
			Name:       name,
			Size:       size,
			DataOffset: dataOff,
			iv:         iv,
			Plain:      plain,
		})
		off = dataOff + padded
	}
	return db, nil
}

// FindFile returns the inner file with the given name.
func (db *DataBin) FindFile(name string) *File {
	for _, f := range db.Files {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// Rebuild returns a complete data.bin: the original raw bytes with each file
// re-encrypted from its (possibly modified) plaintext.
func (db *DataBin) Rebuild() ([]byte, error) {
	out := append([]byte(nil), db.raw...)
	for _, f := range db.Files {
		enc, err := aesCBCEncrypt(SDKey[:], f.iv, f.Plain)
		if err != nil {
			return nil, fmt.Errorf("encrypting %q: %w", f.Name, err)
		}
		copy(out[f.DataOffset:f.DataOffset+len(enc)], enc)
	}
	return out, nil
}

//== AES-128-CBC helpers

// aesCBCDecrypt decrypts in (whose length must be a multiple of the AES block).
func aesCBCDecrypt(key, iv, in []byte) ([]byte, error) {
	if len(in)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("length %d is not a multiple of the AES block", len(in))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(in))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, in)
	return out, nil
}

// aesCBCEncrypt encrypts in (whose length must be a multiple of the AES block).
func aesCBCEncrypt(key, iv, in []byte) ([]byte, error) {
	if len(in)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("length %d is not a multiple of the AES block", len(in))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(in))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, in)
	return out, nil
}

// cString returns b truncated at its first NUL byte, as a string.
func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
