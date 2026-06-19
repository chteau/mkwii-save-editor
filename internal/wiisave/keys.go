package wiisave

// Public Wii keys, long known from the homebrew tools (Segher's Wii.git,
// tachtig/twintig). They are NOT secret: they are constants shared by every
// console and baked into the system software.
//
//   - SDKey : encrypts/decrypts the file contents of a save exported to an SD card
//     (the data.bin format), with AES-128-CBC.
//   - SDIV  : the IV used for the banner (the first 0xF0C0 bytes of the .bin).
//
// Each inner file (rksys.dat, etc.) is encrypted with SDKey and its own IV stored
// in the file header (offset 0x50). In practice that IV is zero.
var (
	SDKey = [16]byte{
		0xab, 0x01, 0xb9, 0xd8, 0xe1, 0x62, 0x2b, 0x08,
		0xaf, 0xba, 0xd8, 0x4d, 0xbf, 0xc2, 0xa5, 0x5d,
	}
	SDIV = [16]byte{
		0x21, 0x67, 0x12, 0xe6, 0xaa, 0x1f, 0x68, 0x9f,
		0x95, 0xc5, 0xa2, 0x23, 0x24, 0xdc, 0x6a, 0x98,
	}
)
