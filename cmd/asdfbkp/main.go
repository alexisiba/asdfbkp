package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alexisiba/asdfbkp/internal/profile"
	"github.com/alexisiba/asdfbkp/internal/tui"
)

// version can be overridden at build time with -ldflags "-X main.version=..."
var version = "0.0.1"

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `asdfbkp %s · general backup manager

Usage:
  asdfbkp [destination]

The backup is saved to <destination>/<device-name>/.
Without a destination, the current directory is used.

Options:
  -v, --version   show the version
  -h, --help      show this help
`, version)
	}
	var showVersion bool
	flag.BoolVar(&showVersion, "v", false, "")
	flag.BoolVar(&showVersion, "version", false, "")
	flag.Parse()

	if showVersion {
		fmt.Println("asdfbkp", version)
		return
	}
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}

	dest := "."
	if flag.NArg() == 1 {
		dest = flag.Arg(0)
	}
	dest, err := filepath.Abs(dest)
	if err != nil {
		fail(err)
	}

	opts := tui.Options{Version: version, Dest: dest}
	// A broken profiles file must not lock the user out: continue without
	// profiles and without saving, so the user's file is never overwritten.
	if path, err := profile.DefaultPath(); err != nil {
		opts.ProfileWarning = "Profiles disabled: " + err.Error()
	} else if opts.Profiles, err = profile.Load(path); err != nil {
		opts.ProfileWarning = "Profiles disabled, check the file: " + err.Error()
	}

	if _, err := tea.NewProgram(tui.New(opts), tea.WithAltScreen()).Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "asdfbkp:", err)
	os.Exit(1)
}
