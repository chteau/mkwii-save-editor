package main

// Interactive (keyboard-driven) mode: a small menu app built on the tui.go
// toolkit. It keeps a working copy of the licenses, mutates it through the menus,
// and applies everything to the save at write time.

import (
	"fmt"
	"os"
	"strings"

	"mkwii-save/internal/wiisave"
)

const appTitle = "🏁 mkwii-save — Mario Kart Wii save editor"

// app holds the state of an interactive session.
type app struct {
	t     *terminal
	path  string
	save  *wiisave.Save
	lics  []wiisave.License // working state, mutated by the edits
	cur   int               // active license
	dirty bool
}

// runInteractive sets up the terminal and runs the main loop until the user quits.
func runInteractive(initial string) {
	t, err := newTerminal()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer t.restore()

	a := &app{t: t}
	if initial != "" {
		if e := a.open(initial); e != nil {
			a.t.message([]string{bold(appTitle)}, "Cannot open "+initial+": "+e.Error())
		}
	}
	for {
		if a.save == nil {
			if !a.openFlow() {
				return
			}
			continue
		}
		if !a.mainMenu() {
			return
		}
	}
}

//== Opening files

// open loads a file into the session working state.
func (a *app) open(path string) error {
	save, err := openFile(path)
	if err != nil {
		return err
	}
	a.path = path
	a.save = save
	a.lics = save.RK.Licenses()
	a.cur = firstUsed(a.lics)
	a.dirty = false
	return nil
}

// openFlow prompts for a path until one opens, or returns false to quit.
func (a *app) openFlow() bool {
	header := []string{bold(appTitle), "", "Open a save file (data.bin or rksys.dat).", "Leave empty to quit."}
	for {
		p, ok := a.t.field(header, "Path", "", false)
		if !ok || strings.TrimSpace(p) == "" {
			return false
		}
		if err := a.open(strings.TrimSpace(p)); err != nil {
			a.t.message(header, "✗ "+err.Error())
			continue
		}
		return true
	}
}

//== Main menu

// header is the banner (file, active license, dirty flag) shown above every menu.
func (a *app) header() []string {
	kind := "rksys.dat"
	if a.save.Kind == "databin" {
		kind = "data.bin"
	}
	dirtyMark := ""
	if a.dirty {
		dirtyMark = "   ● unsaved changes"
	}
	return []string{
		bold(appTitle),
		dim(a.path),
		fmt.Sprintf("%s · active license %s%s", kind, licDesc(a.lics[a.cur]), dirtyMark),
		"",
	}
}

// mainMenu shows the top-level menu and dispatches. Returns false to quit the app.
func (a *app) mainMenu() bool {
	items := []string{
		"View full summary",
		"Switch license",
		"Identity (name, VR, BR, races)",
		"Unlocks (characters / vehicles)",
		"Cup star ranks",
		"Time Trial records",
		"Save",
		"Open another file",
		"Quit",
	}
	switch a.t.menu(a.header(), items, 0) {
	case 0:
		a.viewSummary()
	case 1:
		a.switchLicense()
	case 2:
		a.editIdentity()
	case 3:
		a.editUnlocks()
	case 4:
		a.editCups()
	case 5:
		a.editRecords()
	case 6:
		a.doSave()
	case 7:
		if !a.openFlow() {
			a.save = nil // back to open prompt
		}
	case 8, -1:
		return a.confirmQuit()
	}
	return true
}

// confirmQuit asks what to do about unsaved changes. Returns true to quit the app.
func (a *app) confirmQuit() bool {
	if !a.dirty {
		return false
	}
	switch a.t.menu([]string{bold("Unsaved changes")}, []string{"Save and quit", "Quit without saving", "Cancel"}, 2) {
	case 0:
		a.doSave()
		return !a.dirty // quit only if the save succeeded
	case 1:
		return true
	default:
		return false
	}
}

//== Summary (read-only pager)

// viewSummary shows the full save dump in a scrollable pager.
func (a *app) viewSummary() {
	a.t.pager([]string{bold(appTitle), ""}, summaryLines(a.path, a.save, a.lics))
}

// pager scrolls read-only lines; it lives here as it is app-specific.
func (t *terminal) pager(header, lines []string) {
	top := 0
	for {
		avail := t.rows - len(header) - 2
		if avail < 3 {
			avail = 3
		}
		if max := len(lines) - avail; top > max {
			top = max
		}
		if top < 0 {
			top = 0
		}
		var b strings.Builder
		b.WriteString("\033[2J\033[H")
		for _, h := range header {
			b.WriteString(h)
			b.WriteString("\r\n")
		}
		end := top + avail
		if end > len(lines) {
			end = len(lines)
		}
		for i := top; i < end; i++ {
			b.WriteString(lines[i])
			b.WriteString("\r\n")
		}
		b.WriteString("\r\n" + dim("↑/↓ scroll · Esc/q back"))
		fmt.Print(b.String())

		k, r := t.readKey()
		switch {
		case k == keyUp || (k == keyChar && r == 'k'):
			top--
		case k == keyDown || (k == keyChar && r == 'j'):
			top++
		case k == keyBack || k == keyLeft || (k == keyChar && r == 'q'):
			return
		}
	}
}

//== License picker

// switchLicense lets the user pick which (used) license to edit.
func (a *app) switchLicense() {
	var items []string
	for _, l := range a.lics {
		items = append(items, licDesc(l))
	}
	n := a.t.menu(a.header(), items, a.cur)
	if n < 0 {
		return
	}
	if !a.lics[n].Used {
		a.t.message(a.header(), "That license is empty.")
		return
	}
	a.cur = n
}

//== Identity (name / VR / BR / races)

// editIdentity is the submenu for the scalar license fields.
func (a *app) editIdentity() {
	for {
		l := &a.lics[a.cur]
		items := []string{
			"Name   : " + l.Name,
			fmt.Sprintf("VR     : %d", l.VR),
			fmt.Sprintf("BR     : %d", l.BR),
			fmt.Sprintf("Races  : %d", l.RaceCount),
		}
		switch a.t.menu(a.header(), items, 0) {
		case 0:
			if v, ok := a.t.field(a.header(), "Mii name (max 10)", l.Name, false); ok {
				l.Name = v
				a.dirty = true
			}
		case 1:
			a.editIntField(&l.VR, "VR (0-65535)", 0, 65535)
		case 2:
			a.editIntField(&l.BR, "BR (0-65535)", 0, 65535)
		case 3:
			a.editIntField(&l.RaceCount, "Total races", 0, 1<<30)
		default:
			return
		}
	}
}

// editIntField prompts for an integer and clamps it into [lo, hi].
func (a *app) editIntField(dst *int, label string, lo, hi int) {
	v, ok := a.t.field(a.header(), label, fmt.Sprintf("%d", *dst), true)
	if !ok || strings.TrimSpace(v) == "" {
		return
	}
	n := atoiOr(v, *dst)
	if n < lo {
		n = lo
	}
	if n > hi {
		n = hi
	}
	*dst = n
	a.dirty = true
}

//== Unlocks (characters / vehicles)

// editUnlocks is the submenu for character/vehicle unlocks.
func (a *app) editUnlocks() {
	for {
		items := []string{"Unlock everything", "Lock everything", "Characters…", "Vehicles…"}
		switch a.t.menu(a.header(), items, 0) {
		case 0:
			for i := range a.lics[a.cur].UnlockBytes {
				a.lics[a.cur].UnlockBytes[i] = 0xFF
			}
			a.dirty = true
		case 1:
			for i := range a.lics[a.cur].UnlockBytes {
				a.lics[a.cur].UnlockBytes[i] = 0x00
			}
			a.dirty = true
		case 2:
			a.toggleItems("Characters", wiisave.Characters)
		case 3:
			a.toggleItems("Vehicles", wiisave.Vehicles)
		default:
			return
		}
	}
}

// toggleItems shows a checklist for a set of unlock items and writes the result
// back into the active license's unlock bytes.
func (a *app) toggleItems(title string, items []wiisave.UnlockItem) {
	bytes := &a.lics[a.cur].UnlockBytes
	names := make([]string, len(items))
	checked := make([]bool, len(items))
	for i, it := range items {
		names[i] = it.Name
		checked[i] = it.Has(*bytes)
	}
	a.t.checklist(append(a.header(), bold(title)), names, checked)
	for i, it := range items {
		mask := byte(0x80 >> it.Bit)
		if checked[i] {
			bytes[it.Byte] |= mask
		} else {
			bytes[it.Byte] &^= mask
		}
	}
	a.dirty = true
}

//== Cup star ranks

// editCups lists the 32 cups and lets the user edit ranks (or bulk set/clear).
func (a *app) editCups() {
	for {
		l := &a.lics[a.cur]
		items := []string{"★ Set ALL to 3★ Gold", "Clear ALL"}
		for _, c := range l.Cups {
			if c.Rank == 15 {
				items = append(items, fmt.Sprintf("%-16s —", c.Label))
			} else {
				items = append(items, fmt.Sprintf("%-16s %-7s %s", c.Label, trophyLabel(c.Trophy), rankLabel(c.Rank)))
			}
		}
		header := append(a.header(), dim("Cups COMPLETED in Grand Prix (rank earned), not just unlocked."))
		n := a.t.menu(header, items, 0)
		switch {
		case n < 0:
			return
		case n == 0:
			for i := range l.Cups {
				l.Cups[i].Rank, l.Cups[i].Trophy = 0, 0
			}
			a.dirty = true
		case n == 1:
			for i := range l.Cups {
				l.Cups[i].Rank, l.Cups[i].Trophy = 15, 3
			}
			a.dirty = true
		default:
			a.editOneCup(&l.Cups[n-2]) // -2 for the two action rows
		}
	}
}

// editOneCup prompts for a cup's rank and (if completed) its trophy.
func (a *app) editOneCup(c *wiisave.CupRank) {
	rankOpts := []string{"3★", "2★", "1★", "A", "B", "C", "D", "E", "F", "— (not completed)"}
	rankVals := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 15}
	sel := 9
	for i, v := range rankVals {
		if v == c.Rank {
			sel = i
		}
	}
	r := a.t.menu(append(a.header(), bold(c.Label+" — rank")), rankOpts, sel)
	if r < 0 {
		return
	}
	c.Rank = rankVals[r]
	a.dirty = true
	if c.Rank == 15 { // not completed -> no trophy
		c.Trophy = 3
		return
	}
	ts := c.Trophy
	if ts < 0 || ts > 2 {
		ts = 0
	}
	if t := a.t.menu(append(a.header(), bold(c.Label+" — trophy")), []string{"Gold", "Silver", "Bronze"}, ts); t >= 0 {
		c.Trophy = t
	}
}

//== Time Trial records

// editRecords lists the existing records and lets the user retime them.
func (a *app) editRecords() {
	l := &a.lics[a.cur]
	if len(l.Records) == 0 {
		a.t.message(a.header(), "No Time Trial records on this license.")
		return
	}
	for {
		var items []string
		for _, r := range l.Records {
			ghost := ""
			if r.HasGhost {
				ghost = "  [ghost]"
			}
			items = append(items, fmt.Sprintf("%-22s %d:%02d.%03d%s", r.Track, r.Min, r.Sec, r.Ms, ghost))
		}
		n := a.t.menu(a.header(), items, 0)
		if n < 0 {
			return
		}
		rec := &l.Records[n]
		cur := fmt.Sprintf("%d:%02d.%03d", rec.Min, rec.Sec, rec.Ms)
		v, ok := a.t.field(append(a.header(), bold(rec.Track)), "New time M:SS.mmm", cur, false)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		m, s, ms, err := parseTimeStr(v)
		if err != nil {
			a.t.message(a.header(), "✗ "+err.Error())
			continue
		}
		rec.Min, rec.Sec, rec.Ms = m, s, ms
		a.dirty = true
	}
}

//== Save

// doSave applies the working state to the save, recomputes the checksum, asks for
// a format/path, and writes the file.
func (a *app) doSave() {
	for _, l := range a.lics {
		if !l.Used {
			continue
		}
		edit := wiisave.LicenseEdit{
			Index: l.Index, Name: l.Name, VR: l.VR, BR: l.BR,
			RaceCount: l.RaceCount, Unlocks: intSlice(l.UnlockBytes),
		}
		for _, c := range l.Cups {
			edit.Cups = append(edit.Cups, wiisave.CupRankEdit{Index: c.Index, Trophy: c.Trophy, Rank: c.Rank})
		}
		for _, r := range l.Records {
			edit.Records = append(edit.Records, wiisave.TTRecordEdit{Slot: r.Slot, Min: r.Min, Sec: r.Sec, Ms: r.Ms})
		}
		if err := a.save.RK.Apply(edit); err != nil {
			a.t.message(a.header(), "✗ "+err.Error())
			return
		}
	}
	a.save.RK.FixChecksum()

	format := a.save.Kind
	if format == "databin" {
		opts := []string{"data.bin (encrypted — for Dolphin import)", "rksys.dat (raw — for the NAND folder)"}
		switch a.t.menu(append(a.header(), bold("Output format")), opts, 0) {
		case 1:
			format = "rksys"
		case -1:
			return
		}
	}

	def := editPath(a.path)
	out, ok := a.t.field(append(a.header(), bold("Save as")), "Output path", def, false)
	if !ok {
		return
	}
	out = strings.TrimSpace(out)
	if out == "" {
		out = def
	}
	out = expandHome(out)

	blob, err := a.save.Encode(format)
	if err != nil {
		a.t.message(a.header(), "✗ "+err.Error())
		return
	}
	if err := os.WriteFile(out, blob, 0o644); err != nil {
		a.t.message(a.header(), "✗ write failed: "+err.Error())
		return
	}
	a.dirty = false
	a.t.message(a.header(), fmt.Sprintf("✓ Saved: %s  (%s, %d bytes)", out, format, len(blob)))
}

//== Small helpers

// firstUsed returns the index of the first used license (or 0 if none).
func firstUsed(lics []wiisave.License) int {
	for i, l := range lics {
		if l.Used {
			return i
		}
	}
	return 0
}

// licDesc is a short "<index> (<name|empty>)" label for a license.
func licDesc(l wiisave.License) string {
	if !l.Used {
		return fmt.Sprintf("%d (empty)", l.Index)
	}
	return fmt.Sprintf("%d (%s)", l.Index, l.Name)
}
