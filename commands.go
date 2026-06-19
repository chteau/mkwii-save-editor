package main

// Non-interactive commands ("show" / "set") and the shared formatting/parsing
// helpers also used by the interactive mode.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mkwii-save/internal/wiisave"
)

//== Summary rendering

// printSummary prints the full contents of a save (used by "show").
func printSummary(path string, save *wiisave.Save, lics []wiisave.License) {
	for _, line := range summaryLines(path, save, lics) {
		fmt.Println(line)
	}
}

// summaryLines builds the full save dump as individual lines, so the interactive
// pager and the "show" command share the exact same formatting.
func summaryLines(path string, save *wiisave.Save, lics []wiisave.License) []string {
	kind := "rksys.dat (raw)"
	if save.Kind == "databin" {
		kind = "data.bin (encrypted)"
	}
	crc := "needs recompute"
	if save.RK.ChecksumOK() {
		crc = "OK"
	}
	var out []string
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }

	add("File   : %s", path)
	add("Format : %s — checksum %s", kind, crc)
	if save.Kind == "databin" {
		var fn []string
		for _, f := range save.DB.Files {
			fn = append(fn, f.Name)
		}
		add("Inner files: %s", strings.Join(fn, ", "))
	}

	corners := []string{"top-left", "top-right", "bottom-left", "bottom-right"}
	for _, l := range lics {
		add("")
		if !l.Used {
			add("── License %d (%s) ── empty", l.Index, corners[l.Index])
			continue
		}
		add("── License %d (%s) ── IN USE", l.Index, corners[l.Index])
		add("  Mii name   : %s", l.Name)
		add("  VR / BR    : %d / %d", l.VR, l.BR)
		add("  Races      : %d", l.RaceCount)
		add("  Characters : %s", joinOrNone(wiisave.UnlockedCharacters(l.UnlockBytes)))
		add("  Vehicles   : %s", joinOrNone(wiisave.UnlockedVehicles(l.UnlockBytes)))

		add("  Star ranks — cups COMPLETED in Grand Prix (index for --cup):")
		for _, c := range l.Cups {
			if c.Rank == 15 {
				add("    [%2d] %-16s —", c.Index, c.Label)
			} else {
				add("    [%2d] %-16s %-7s %s", c.Index, c.Label, trophyLabel(c.Trophy), rankLabel(c.Rank))
			}
		}

		add("  Time Trial records (slot for --record):")
		if len(l.Records) == 0 {
			add("    (none)")
		}
		for _, r := range l.Records {
			ghost := ""
			if r.HasGhost {
				ghost = "  [ghost]"
			}
			add("    slot %2d  %-22s %d:%02d.%03d%s", r.Slot, r.Track, r.Min, r.Sec, r.Ms, ghost)
		}
	}
	return out
}

//== Command: show

// cmdShow loads a save and prints its summary.
func cmdShow(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: mkwii-save show <file>")
	}
	if len(args) > 1 {
		return fmt.Errorf("\"show\" only takes the file (extra args: %q).\n"+
			"       To EDIT a cup, use \"set\": mkwii-save set <file> --cup IDX=RANK[,TROPHY]",
			strings.Join(args[1:], " "))
	}
	save, err := openFile(args[0])
	if err != nil {
		return err
	}
	printSummary(args[0], save, save.RK.Licenses())
	return nil
}

//== Command: set

// cmdSet applies the flag-driven edits to a license and writes the result out.
func cmdSet(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: mkwii-save set <file> [options]")
	}
	path := args[0]
	save, err := openFile(path)
	if err != nil {
		return err
	}

	o := parseSetFlags(args[1:])
	if o.err != nil {
		return o.err
	}
	if o.license < 0 || o.license > 3 {
		return fmt.Errorf("invalid license: %d (expected 0-3)", o.license)
	}

	cur := save.RK.Licenses()[o.license]
	if !cur.Used {
		return fmt.Errorf("license %d is empty: nothing to edit", o.license)
	}

	// Start from the current values and only override what was passed.
	edit := wiisave.LicenseEdit{
		Index:     o.license,
		Name:      pick(o.name, cur.Name),
		VR:        pickInt(o.vr, cur.VR),
		BR:        pickInt(o.br, cur.BR),
		RaceCount: pickInt(o.races, cur.RaceCount),
		Unlocks:   intSlice(cur.UnlockBytes),
	}
	switch {
	case o.unlockAll:
		edit.Unlocks = []int{255, 255, 255, 255, 255, 255, 255, 255}
	case o.lockAll:
		edit.Unlocks = []int{0, 0, 0, 0, 0, 0, 0, 0}
	}
	if o.threeStarAll {
		for i := 0; i < 32; i++ {
			edit.Cups = append(edit.Cups, wiisave.CupRankEdit{Index: i, Trophy: 0, Rank: 0})
		}
	}
	edit.Cups = append(edit.Cups, o.cups...)
	edit.Records = o.records

	if err := save.RK.Apply(edit); err != nil {
		return err
	}
	save.RK.FixChecksum()

	format := o.format
	if format == "" {
		format = save.Kind
	}
	out := o.out
	if o.inPlace {
		out = path
	}
	if out == "" {
		out = editPath(path)
	}

	blob, err := save.Encode(format)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, blob, 0o644); err != nil {
		return err
	}
	fmt.Printf("✓ Saved: %s (%s, %d bytes)\n", out, format, len(blob))
	return nil
}

// setOpts holds the parsed flags of the "set" command.
type setOpts struct {
	license                          int
	name                             *string // nil = keep current
	vr, br, races                    *int    // nil = keep current
	unlockAll, lockAll, threeStarAll bool
	inPlace                          bool
	out, format                      string
	cups                             []wiisave.CupRankEdit
	records                          []wiisave.TTRecordEdit
	err                              error
}

// parseSetFlags parses the "set" flags. It stops at the first error (in o.err).
func parseSetFlags(args []string) setOpts {
	o := setOpts{license: 0}
	next := func(i *int, name string) (string, bool) {
		if *i+1 >= len(args) {
			o.err = fmt.Errorf("option %s expects a value", name)
			return "", false
		}
		*i++
		return args[*i], true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-l", "--license":
			if v, ok := next(&i, a); ok {
				o.license = atoiOr(v, 0)
			}
		case "--name":
			if v, ok := next(&i, a); ok {
				o.name = &v
			}
		case "--vr":
			if v, ok := next(&i, a); ok {
				n := atoiOr(v, 0)
				o.vr = &n
			}
		case "--br":
			if v, ok := next(&i, a); ok {
				n := atoiOr(v, 0)
				o.br = &n
			}
		case "--races":
			if v, ok := next(&i, a); ok {
				n := atoiOr(v, 0)
				o.races = &n
			}
		case "--unlock-all":
			o.unlockAll = true
		case "--lock-all":
			o.lockAll = true
		case "--three-star-all":
			o.threeStarAll = true
		case "--in-place":
			o.inPlace = true
		case "-o", "--out":
			if v, ok := next(&i, a); ok {
				o.out = v
			}
		case "--format":
			if v, ok := next(&i, a); ok {
				o.format = v
			}
		case "--cup":
			if v, ok := next(&i, a); ok {
				if c, err := parseCup(v); err != nil {
					o.err = err
				} else {
					o.cups = append(o.cups, c)
				}
			}
		case "--record":
			if v, ok := next(&i, a); ok {
				if r, err := parseRecord(v); err != nil {
					o.err = err
				} else {
					o.records = append(o.records, r)
				}
			}
		default:
			o.err = fmt.Errorf("unknown option: %q", a)
		}
		if o.err != nil {
			return o
		}
	}
	return o
}

// parseCup parses an "IDX=RANK[,TROPHY]" --cup argument.
func parseCup(spec string) (wiisave.CupRankEdit, error) {
	idxStr, rest, ok := strings.Cut(spec, "=")
	if !ok {
		return wiisave.CupRankEdit{}, fmt.Errorf("--cup expects IDX=RANK[,TROPHY] (got %q)", spec)
	}
	idx, err := strconv.Atoi(strings.TrimSpace(idxStr))
	if err != nil || idx < 0 || idx > 31 {
		return wiisave.CupRankEdit{}, fmt.Errorf("invalid cup index: %q (0-31)", idxStr)
	}
	rankStr, trophyStr, _ := strings.Cut(rest, ",")
	rank, err := parseRank(rankStr)
	if err != nil {
		return wiisave.CupRankEdit{}, err
	}
	trophy := 0
	if trophyStr != "" {
		if trophy, err = parseTrophy(trophyStr); err != nil {
			return wiisave.CupRankEdit{}, err
		}
	}
	return wiisave.CupRankEdit{Index: idx, Rank: rank, Trophy: trophy}, nil
}

// parseRecord parses a "SLOT=M:SS.mmm" --record argument.
func parseRecord(spec string) (wiisave.TTRecordEdit, error) {
	slotStr, timeStr, ok := strings.Cut(spec, "=")
	if !ok {
		return wiisave.TTRecordEdit{}, fmt.Errorf("--record expects SLOT=M:SS.mmm (got %q)", spec)
	}
	slot, err := strconv.Atoi(strings.TrimSpace(slotStr))
	if err != nil || slot < 0 || slot > 31 {
		return wiisave.TTRecordEdit{}, fmt.Errorf("invalid slot: %q (0-31)", slotStr)
	}
	m, s, ms, err := parseTimeStr(timeStr)
	if err != nil {
		return wiisave.TTRecordEdit{}, err
	}
	return wiisave.TTRecordEdit{Slot: slot, Min: m, Sec: s, Ms: ms}, nil
}

// parseTimeStr decodes "M:SS.mmm" into minutes/seconds/milliseconds.
func parseTimeStr(s string) (min, sec, ms int, err error) {
	minStr, secMs, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, 0, 0, fmt.Errorf("time expects M:SS.mmm (got %q)", s)
	}
	secStr, msStr, ok := strings.Cut(secMs, ".")
	if !ok {
		return 0, 0, 0, fmt.Errorf("time expects M:SS.mmm (got %q)", s)
	}
	m, e1 := strconv.Atoi(strings.TrimSpace(minStr))
	se, e2 := strconv.Atoi(strings.TrimSpace(secStr))
	mi, e3 := strconv.Atoi(strings.TrimSpace(msStr))
	if e1 != nil || e2 != nil || e3 != nil {
		return 0, 0, 0, fmt.Errorf("non-numeric time: %q", s)
	}
	return m, se, mi, nil
}

//== Labels

var rankNames = []string{"3★", "2★", "1★", "A", "B", "C", "D", "E", "F"}
var trophyNames = []string{"Gold", "Silver", "Bronze"}

// rankLabel returns the display label for a rank value (0-8), or "—" otherwise.
func rankLabel(r int) string {
	if r >= 0 && r < len(rankNames) {
		return rankNames[r]
	}
	return "—"
}

// trophyLabel returns the display label for a trophy value (0-2), or "—" otherwise.
func trophyLabel(t int) string {
	if t >= 0 && t < len(trophyNames) {
		return trophyNames[t]
	}
	return "—"
}

// parseRank converts a rank token (3star, a, none, …) to its value (0-8, or 15).
func parseRank(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "3star", "3stars", "3★", "***":
		return 0, nil
	case "2star", "2stars", "2★", "**":
		return 1, nil
	case "1star", "1stars", "1★", "*":
		return 2, nil
	case "a":
		return 3, nil
	case "b":
		return 4, nil
	case "c":
		return 5, nil
	case "d":
		return 6, nil
	case "e":
		return 7, nil
	case "f":
		return 8, nil
	case "none", "-", "":
		return 15, nil
	}
	return 0, fmt.Errorf("unknown rank: %q (3star 2star 1star a b c d e f none)", s)
}

// parseTrophy converts a trophy token to its value (0-2, or 3 for none).
func parseTrophy(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "gold":
		return 0, nil
	case "silver":
		return 1, nil
	case "bronze":
		return 2, nil
	case "none", "-":
		return 3, nil
	}
	return 0, fmt.Errorf("unknown trophy: %q (gold silver bronze none)", s)
}

//== Misc helpers

// openFile reads a save file (expanding a leading ~/) and parses it.
func openFile(path string) (*wiisave.Save, error) {
	raw, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, err
	}
	return wiisave.Open(raw)
}

// expandHome expands a leading "~/" to the user's home directory.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// editPath inserts ".edit" before the extension so the source is not overwritten.
func editPath(p string) string {
	ext := filepath.Ext(p)
	return strings.TrimSuffix(p, ext) + ".edit" + ext
}

// joinOrNone joins items with ", ", or returns "(none)" when the list is empty.
func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return strings.Join(s, ", ")
}

// intSlice converts the 8 unlock bytes to an []int (the LicenseEdit shape).
func intSlice(b [8]byte) []int {
	out := make([]int, 8)
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}

// pick returns *p if non-nil, else def.
func pick(p *string, def string) string {
	if p != nil {
		return *p
	}
	return def
}

// pickInt returns *p if non-nil, else def.
func pickInt(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}

// atoiOr parses s as an int, returning def on failure.
func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
