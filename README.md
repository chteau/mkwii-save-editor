# 🏁 Mario Kart Wii Save Editor

A simple portable TUI save editor for **Mario Kart Wii** written in Go made in my spare time.

It reads and writes both formats:

- **`data.bin`** — the save as exported to an SD card (`private/wii/title/RMCP/data.bin`), **encrypted** with AES-128-CBC.
- **`rksys.dat`** — the raw (decrypted) save, the way Dolphin stores it in its NAND (`Wii/title/00010004/524d4350/data/rksys.dat`).


## Features

Per "license" profile, you can edit:

- The **Mii name**, **VR** (Versus Rating), **BR** (Battle Rating), **race count**;
- All **unlocks** (characters, vehicles), with "unlock/lock everything";
- The **cup star ranks** (trophy Gold/Silver/Bronze + rank 3★…F) for every cup and
  Engine class (50/100/150/Mirror), with "set all to 3★ Gold";
- The **Time Trial records**: the displayed time of each track, with **automatic sync of the matching ghost**

## Interactive mode (default)

Just run the binary in a shell and it should do the work:

```sh
./mkwii-save
```

You can also open a file directly:

```sh
./mkwii-save ~/Documents/Dolphin/private/wii/title/RMCP/data.bin
```

## Non-interactive mode (scriptable)

```sh
# Prints (licenses, VR/BR, unlocks, star ranks with index, and all the other things it has)
./mkwii-save show data.bin

# Edit usic parameters
./mkwii-save set data.bin --vr 9999 --unlock-all --three-star-all
./mkwii-save set data.bin -l 0 --name MARIO --record 0=1:11.111 --cup 16=3star,gold -o out.bin
```

> Run with `--help` for a list of all the parameters you can use on that.

> **Star ranks vs unlocks:** the star ranks are the cups you **completed in Grand Prix** when playing normally, which is different from the cups that are **unlocked**. Basically, a cup you unlocked doesn't necessarily got a rank if you didn't play on it yet.

## Using it with Dolphin

1. **Re-import the `.bin`**: save in `databin` format, then in Dolphin *Tools → Import Wii Save…* and pick the file. Verified in Dolphin's source: on import it does **not** check the ECDSA signature (it only reads the headers and decrypts with the SD key), so the re-edited file is accepted even though its original signature no longer matches.

2. **Drop the `rksys.dat`**: save in `rksys` format and copy it into Dolphin's NAND folder (`User/Wii/title/00010004/524d4350/data/rksys.dat`). No signature involved.

> Just remember to make a backup or your save if you don't feel like losing all your progress dealing with 5 red shells in a row and a f*cking blue shell right before the finish line.

## Build

> Make sure you got go installed on your machine. 

```sh
cd ~/whereveryoucloned/mkwii-save
go build -o mkwii-save .        # the CLI binary
go test ./internal/wiisave/     # tests against a real data.bin
```

## Format (reverse-engineered)

All was verified using a real MKWii save. Wii keys are public from the Wii Homebrew Channel.

### data.bin

| Offset    | Contents                                                       |
|-----------|----------------------------------------------------------------|
| `0x0000`  | Banner, `0xF0C0` bytes, AES-128-CBC (SD key, SD IV)            |
| `0xF0C0`  | `Bk` (backup) header, `0x80` bytes, **plaintext**             |
| `0xF140`  | Per file: `0x80` plaintext header + encrypted data padded to `0x40`; IV at header offset `0x50` (zero) |
| end       | Footer: certificates + ECDSA signature                         |

Inner files of a MKWii save: `wc24scr.vff`, `wc24dl.vff`, `rksys.dat`.

### rksys.dat (fixed size `0x2BC000`)

| Offset    | Contents                                                       |
|-----------|----------------------------------------------------------------|
| `0x00000` | `"RKSD"` + version `"0006"`                                    |
| `0x00008` | License 0 (`RKPD`), `0x8CC0` bytes                            |
| `0x08CC8` | License 1                                                      |
| `0x11988` | License 2                                                      |
| `0x1A648` | License 3                                                      |
| `0x27FFC` | **CRC-32** (zlib/IEEE, big-endian) of `[0x0 : 0x27FFC]`        |
| `0x28000` | Ghost data (`RKGD`)                                            |

Fields inside a license (offsets relative to the start of the `RKPD` block):

| Rel.   | Type      | Field                                  |
|--------|-----------|----------------------------------------|
| `0x14` | UTF-16BE  | Mii name (`0x14` bytes, 10 chars)      |
| `0x30`–`0x37` | bits | Unlock bitfields (cups / characters / vehicles) |
| `0xB0` | u16       | VR (Versus Rating)                     |
| `0xB2` | u16       | BR (Battle Rating)                     |
| `0xB4` | u32       | Total race count                       |

> The unlock bit→item mapping uses the wiki's **MSB** numbering (`bit ".N" has mask 0x80 >> N`), verified on real saves (e.g. Tiny Titan = `0x37.4` = mask `0x08`). "Unlock everything" sets the 8 bytes to `0xFF`, which unlocks all regardless.

### Cup star ranks ("Cup Data" section)

32 entries of `0x60` bytes starting at `license + 0x1C0`, indexed by `cc*8 + cup`
(cc: 0=50cc, 1=100cc, 2=150cc, 3=Mirror; cup 0..7 = Mushroom, Flower, Star,
Special, Shell, Banana, Leaf, Lightning).

Encoding **determined empirically** (the wiki was ambiguous and uses MSB bit
numbering here). **A non-completed cup has its whole entry zeroed** — so you must
test the completion bit, otherwise an empty cup reads wrongly as "3★ Gold"
(rank 0 / trophy 0).

| Rel.   | Field                                                  |
|--------|--------------------------------------------------------|
| `0x4F` | Trophy (2 **high** bits): 0=Gold 1=Silver 2=Bronze 3=none |
| `0x51` | Rank (**low** nibble): 0=3★ 1=2★ 2=1★ 3=A … 8=F        |
| `0x52` | bit 7 (`0x80`): **cup completed** (else rank/trophy not shown) |

### Time Trial records ("Time Trial Leaderboards" section)

The "rank 1" block at `license + 0xDC0`: 32 entries of `0x60` (one per track, in cup
order). The time is at `+0x4C`, packed in 24 bits: **7-bit minutes, 7-bit seconds,
10-bit milliseconds** (big-endian).

The matching PB ghost is an **RKG** file stored at
`0x28000 + license*0xA5000 + track*0x2800`:

| Offset (in the slot) | Field                                              |
|----------------------|----------------------------------------------------|
| `0x00`               | `RKGD` magic                                        |
| `0x04`               | finish time (same 7/7/10 format)                   |
| `0x27FC`             | **CRC-32** (zlib/IEEE) of the ghost over `slot[0:0x27FC]` |

When you change a time, the editor updates `0x4C` (leaderboard) **and** `0x04` of
the ghost, then recomputes its CRC at `0x27FC`.

That's basically all. Y'all have fun huh.

## Credits

- Wii keys and save format: Segher's homebrew tools (`tachtig`/`twintig`), WiiBrew.
- `rksys.dat` structure: Custom Mario Kart wiki (tockdom).
