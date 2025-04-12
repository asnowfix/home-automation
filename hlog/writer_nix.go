//go:build !windows
// +build !windows

package hlog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattn/go-isatty"
)

func debugInit(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}

func IsConsole() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

func getLogDir() string {
	// If running as root, use /var/log
	if os.Geteuid() == 0 {
		return "/var/log/myhome"
	}

	// Otherwise use XDG_STATE_HOME or ~/.local/state
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		stateDir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateDir, "myhome", "logs")
}
