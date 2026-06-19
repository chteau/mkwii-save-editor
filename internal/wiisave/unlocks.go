package wiisave

// Mapping of unlockables (characters / vehicles) inside a license's 8 unlock bytes
// (offsets 0x30..0x37, here Byte = index 0..7).
//
// The save numbers bits MSB-first: the wiki's ".N" bit has mask 0x80 >> N.
// Verified on real saves (e.g. Tiny Titan = 0x37.4 = mask 0x08,
// King Boo = 0x32.6 = mask 0x02).

// UnlockItem describes an unlockable and the position of its bit.
type UnlockItem struct {
	Name string
	Byte int // index 0..7 within the unlock bytes
	Bit  int // wiki ".N" number (mask = 0x80 >> Bit)
}

// Characters lists the unlockable characters and their unlock bits.
var Characters = []UnlockItem{
	{"Mii (Outfit B)", 2, 2}, {"Mii (Outfit A)", 2, 3}, {"Rosalina", 2, 4},
	{"Funky Kong", 2, 5}, {"King Boo", 2, 6}, {"Dry Bowser", 2, 7},
	{"Birdo", 3, 0}, {"Daisy", 3, 1}, {"Bowser Jr.", 3, 2}, {"Diddy Kong", 3, 3},
	{"Baby Luigi", 3, 4}, {"Baby Daisy", 3, 5}, {"Toadette", 3, 6}, {"Dry Bones", 3, 7},
}

// Vehicles lists the unlockable vehicles and their unlock bits.
var Vehicles = []UnlockItem{
	{"Phantom", 5, 4}, {"Spear", 5, 5}, {"Shooting Star", 5, 6}, {"Dolphin Dasher", 5, 7},
	{"Sneakster", 6, 0}, {"Zip Zip", 6, 1}, {"Jet Bubble", 6, 2}, {"Magikruiser", 6, 3},
	{"Quacker", 6, 4}, {"Honeycoupe", 6, 5}, {"Jetsetter", 6, 6}, {"Piranha Prowler", 6, 7},
	{"Sprinter", 7, 0}, {"Daytripper", 7, 1}, {"Super Blooper", 7, 2}, {"Blue Falcon", 7, 3},
	{"Tiny Titan", 7, 4}, {"Cheep Charger", 7, 5},
}

// Has reports whether the item is unlocked in these bytes.
func (u UnlockItem) Has(b [8]byte) bool {
	return b[u.Byte]&(0x80>>u.Bit) != 0
}

// names returns the unlocked names from a list.
func names(items []UnlockItem, b [8]byte) []string {
	var out []string
	for _, it := range items {
		if it.Has(b) {
			out = append(out, it.Name)
		}
	}
	return out
}

// UnlockedCharacters returns the names of the unlocked characters in b.
func UnlockedCharacters(b [8]byte) []string { return names(Characters, b) }

// UnlockedVehicles returns the names of the unlocked vehicles in b.
func UnlockedVehicles(b [8]byte) []string { return names(Vehicles, b) }
