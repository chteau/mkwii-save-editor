// Package wiisave reads and edits Mario Kart Wii save files.
//
// It handles both on-disk forms of the save:
//
//   - data.bin  — the SD-card export, AES-128-CBC encrypted with the public Wii
//     "SD key". [DataBin] unpacks it (banner / Bk header / inner files / footer)
//     and re-encrypts it in place, so a no-edit round-trip is byte-identical.
//   - rksys.dat — the raw save the game reads. [Rksys] parses its 4 licenses and
//     exposes the editable fields, and recomputes the CRC-32 at 0x27FFC.
//
// [Open] auto-detects the format and returns a [Save]; [Save.Encode] writes it
// back out. The editable surface (name, VR/BR, races, unlocks, cup star ranks,
// Time Trial records) is applied through [Rksys.Apply].
//
// All offsets and the encryption were reverse-engineered and verified against a
// real save; see the per-file comments for the on-disk layout.
package wiisave
