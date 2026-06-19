package main

// Minimal, dependency-free terminal UI toolkit: raw mode via `stty`, an alternate
// screen buffer, key reading (arrows / Enter / Esc / Backspace / Space) and a few
// widgets (menu, checklist, text field).

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

//== Terminal control

// terminal owns the raw-mode terminal state and buffered input reader.
type terminal struct {
	in       *bufio.Reader
	oldState string // stty state to restore on exit
	rows     int
	restored bool
}

// newTerminal switches the terminal to raw mode, enters the alternate screen and
// hides the cursor. It fails if stdout is not a TTY.
func newTerminal() (*terminal, error) {
	if !isTTY() {
		return nil, fmt.Errorf("interactive mode needs a terminal — use `mkwii-save show <file>` / `set` instead")
	}
	old, err := sttyGet("-g")
	if err != nil {
		return nil, fmt.Errorf("cannot read terminal state: %w", err)
	}
	if err := stty("-icanon", "-echo", "-isig", "min", "1", "time", "0"); err != nil {
		return nil, fmt.Errorf("cannot set raw mode: %w", err)
	}
	t := &terminal{in: bufio.NewReader(os.Stdin), oldState: strings.TrimSpace(old), rows: termRows()}
	fmt.Print("\033[?1049h\033[?25l") // enter alt screen, hide cursor
	t.handleSignals()
	return t, nil
}

// restore puts the terminal back to its original state (idempotent).
func (t *terminal) restore() {
	if t.restored {
		return
	}
	t.restored = true
	fmt.Print("\033[?25h\033[?1049l") // show cursor, leave alt screen
	if t.oldState != "" {
		stty(t.oldState)
	}
}

// quit restores the terminal and exits the process cleanly.
func (t *terminal) quit() {
	t.restore()
	os.Exit(0)
}

// handleSignals restores the terminal if the process is terminated by a signal.
func (t *terminal) handleSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP)
	go func() { <-ch; t.restore(); os.Exit(1) }()
}

//== Key input

// key is a decoded keypress.
type key int

const (
	keyUp key = iota
	keyDown
	keyLeft
	keyRight
	keyEnter
	keyBack // Esc / Backspace
	keySpace
	keyChar // a printable rune is returned alongside
	keyUnknown
)

// readKey reads and decodes one keypress. Ctrl+C and EOF quit immediately.
func (t *terminal) readKey() (key, rune) {
	b, err := t.in.ReadByte()
	if err != nil {
		t.quit() // EOF
	}
	switch b {
	case 3: // Ctrl+C -> hard quit
		t.quit()
	case 13, 10:
		return keyEnter, 0
	case 127, 8:
		return keyBack, 0
	case 32:
		return keySpace, 0
	case 27: // ESC, possibly an arrow sequence (ESC [ A/B/C/D)
		if t.in.Buffered() >= 2 {
			t.in.ReadByte() // '['
			c, _ := t.in.ReadByte()
			switch c {
			case 'A':
				return keyUp, 0
			case 'B':
				return keyDown, 0
			case 'C':
				return keyRight, 0
			case 'D':
				return keyLeft, 0
			}
			return keyUnknown, 0
		}
		return keyBack, 0 // lone ESC
	}
	if b >= 32 && b < 127 {
		return keyChar, rune(b)
	}
	return keyUnknown, 0
}

//== Rendering

// renderList draws a header, a (possibly scrolled) list with the selected row
// highlighted, optional checkbox marks, and a controls footer.
func (t *terminal) renderList(header, items []string, sel int, marks []bool, controls string) {
	avail := t.rows - len(header) - 3
	if avail < 3 {
		avail = 3
	}
	start := 0
	if len(items) > avail { // scroll so the selection stays visible
		start = sel - avail/2
		if start < 0 {
			start = 0
		}
		if start > len(items)-avail {
			start = len(items) - avail
		}
	}
	end := start + avail
	if end > len(items) {
		end = len(items)
	}

	var b strings.Builder
	b.WriteString("\033[2J\033[H") // clear screen, cursor home
	for _, h := range header {
		b.WriteString(h)
		b.WriteString("\r\n")
	}
	if start > 0 {
		b.WriteString(dim("   ↑ more…"))
		b.WriteString("\r\n")
	}
	for i := start; i < end; i++ {
		line := items[i]
		if marks != nil {
			box := "[ ] "
			if marks[i] {
				box = "[x] "
			}
			line = box + line
		}
		if i == sel {
			b.WriteString("\033[36m▸ \033[7m")
			b.WriteString(line)
			b.WriteString("\033[0m")
		} else {
			b.WriteString("  ")
			b.WriteString(line)
		}
		b.WriteString("\r\n")
	}
	if end < len(items) {
		b.WriteString(dim("   ↓ more…"))
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(dim(controls))
	fmt.Print(b.String())
}

//== Widgets

// menu shows a single-choice list. Returns the chosen index, or -1 on back.
func (t *terminal) menu(header, items []string, sel int) int {
	if len(items) == 0 {
		return -1
	}
	if sel < 0 || sel >= len(items) {
		sel = 0
	}
	for {
		t.renderList(header, items, sel, nil, "↑/↓ move · Enter select · Esc/q back")
		k, r := t.readKey()
		switch {
		case k == keyUp || (k == keyChar && r == 'k'):
			sel = (sel - 1 + len(items)) % len(items)
		case k == keyDown || (k == keyChar && r == 'j'):
			sel = (sel + 1) % len(items)
		case k == keyEnter || k == keyRight:
			return sel
		case k == keyBack || k == keyLeft || (k == keyChar && r == 'q'):
			return -1
		}
	}
}

// checklist toggles items in `checked` in place. Returns when the user goes back.
func (t *terminal) checklist(header, items []string, checked []bool) {
	sel := 0
	for {
		t.renderList(header, items, sel, checked, "↑/↓ move · Space toggle · a all · n none · Esc/q done")
		k, r := t.readKey()
		switch {
		case k == keyUp || (k == keyChar && r == 'k'):
			sel = (sel - 1 + len(items)) % len(items)
		case k == keyDown || (k == keyChar && r == 'j'):
			sel = (sel + 1) % len(items)
		case k == keySpace || k == keyEnter:
			checked[sel] = !checked[sel]
		case k == keyChar && r == 'a':
			for i := range checked {
				checked[i] = true
			}
		case k == keyChar && r == 'n':
			for i := range checked {
				checked[i] = false
			}
		case k == keyBack || k == keyLeft || (k == keyChar && r == 'q'):
			return
		}
	}
}

// field is an inline text editor. Returns (value, ok); ok is false if cancelled.
// When digitsOnly is set, only digits are accepted.
func (t *terminal) field(header []string, label, initial string, digitsOnly bool) (string, bool) {
	buf := []rune(initial)
	for {
		var b strings.Builder
		b.WriteString("\033[2J\033[H")
		for _, h := range header {
			b.WriteString(h)
			b.WriteString("\r\n")
		}
		b.WriteString("\r\n  ")
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(string(buf))
		b.WriteString("\033[7m \033[0m")
		b.WriteString("\r\n\r\n")
		b.WriteString(dim("Enter confirm · Esc cancel · Backspace erase"))
		fmt.Print(b.String())

		c, err := t.in.ReadByte()
		if err != nil {
			t.quit()
		}
		switch {
		case c == 3:
			t.quit()
		case c == 13 || c == 10:
			return string(buf), true
		case c == 27:
			if t.in.Buffered() >= 2 { // swallow an arrow sequence
				t.in.ReadByte()
				t.in.ReadByte()
				continue
			}
			return initial, false // lone ESC = cancel
		case c == 127 || c == 8:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
			}
		case c >= 32 && c < 127:
			if !digitsOnly || (c >= '0' && c <= '9') {
				buf = append(buf, rune(c))
			}
		}
	}
}

// message shows a one-line screen and waits for any keypress.
func (t *terminal) message(header []string, msg string) {
	t.renderList(header, []string{msg}, -1, nil, "press any key…")
	t.readKey()
}

//== Low-level helpers

// dim wraps s in the ANSI dim attribute.
func dim(s string) string { return "\033[2m" + s + "\033[0m" }

// bold wraps s in the ANSI bold attribute.
func bold(s string) string { return "\033[1m" + s + "\033[0m" }

// isTTY reports whether standard output is a terminal.
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// stty runs `stty` against the controlling terminal (stdin) with the given args.
func stty(args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// sttyGet runs `stty <arg>` and returns its stdout.
func sttyGet(arg string) (string, error) {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return string(out), err
}

// termRows returns the terminal height in rows (defaulting to 24).
func termRows() int {
	out, err := sttyGet("size")
	if err == nil {
		if f := strings.Fields(out); len(f) == 2 {
			if n, err := strconv.Atoi(f[0]); err == nil && n > 0 {
				return n
			}
		}
	}
	return 24
}
