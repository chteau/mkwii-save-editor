// mkwii-save: a command-line Mario Kart Wii save editor.
//
// With no argument it launches an INTERACTIVE mode (keyboard-driven menus): open a
// data.bin or rksys.dat, edit, save — all without re-running the command.
//
//	mkwii-save                 # interactive mode
//	mkwii-save <file>          # interactive mode, opening that file
//	mkwii-save show <file>     # non-interactive dump (scriptable)
//	mkwii-save set  <file> [options]   # non-interactive edit (scriptable)
package main

import (
	"fmt"
	"os"
)

// main dispatches to the show/set subcommands, or falls back to interactive mode.
func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "show", "s":
			exit(cmdShow(args[1:]))
		case "set", "edit", "e":
			exit(cmdSet(args[1:]))
		case "help", "-h", "--help":
			usage()
			return
		}
	}
	// Everything else (no argument, or a path) -> interactive mode.
	initial := ""
	if len(args) > 0 {
		initial = args[0]
	}
	runInteractive(initial)
}

// exit prints err to stderr (if any) and terminates with the matching status code.
func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// usage prints the command-line help.
func usage() {
	fmt.Print(`mkwii-save — Mario Kart Wii save editor

  mkwii-save                 interactive mode (menus) — recommended
  mkwii-save <file>          open that file directly in interactive mode
  mkwii-save show <file>     print the contents (non-interactive)
  mkwii-save set  <file> [options]   edit non-interactively (scriptable)

set — options: --license N  --name  --vr  --br  --races  --unlock-all
  --lock-all  --three-star-all  --cup IDX=RANK[,TROPHY]  --record SLOT=M:SS.mmm
  -o/--out  --in-place  --format databin|rksys

The file may be a data.bin (encrypted) or a rksys.dat (raw).
`)
}
