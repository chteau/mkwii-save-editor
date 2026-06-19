package wiisave

import "fmt"

// Save is a loaded save file, whether it comes from a data.bin (encrypted) or a
// rksys.dat (raw). It centralizes loading/encoding so every front-end shares the
// exact same logic.
type Save struct {
	Kind string   // "databin" or "rksys"
	DB   *DataBin // non-nil only when Kind == "databin"
	RK   *Rksys
}

// Open detects the format from the raw bytes and unpacks it.
func Open(raw []byte) (*Save, error) {
	if len(raw) >= 4 && string(raw[:4]) == rksdMagic {
		rk, err := ParseRksys(raw)
		if err != nil {
			return nil, err
		}
		return &Save{Kind: "rksys", RK: rk}, nil
	}
	db, err := ParseDataBin(raw)
	if err != nil {
		return nil, err
	}
	f := db.FindFile("rksys.dat")
	if f == nil {
		return nil, fmt.Errorf("this data.bin has no rksys.dat (not a Mario Kart Wii save?)")
	}
	rk, err := ParseRksys(f.Plain[:RksysSize])
	if err != nil {
		return nil, fmt.Errorf("invalid inner rksys.dat: %w", err)
	}
	return &Save{Kind: "databin", DB: db, RK: rk}, nil
}

// Encode produces the final bytes in the requested format ("databin" or "rksys"),
// after edits and the checksum have been applied to s.RK.
func (s *Save) Encode(format string) ([]byte, error) {
	switch format {
	case "rksys":
		return s.RK.Bytes(), nil
	case "databin":
		if s.DB == nil {
			return nil, fmt.Errorf("no data.bin container loaded: export to rksys.dat, or start from a data.bin")
		}
		if f := s.DB.FindFile("rksys.dat"); f != nil {
			copy(f.Plain, s.RK.Bytes())
		}
		return s.DB.Rebuild()
	}
	return nil, fmt.Errorf("unknown format: %s", format)
}
