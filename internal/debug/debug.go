package debug

import (
	"os"
	"strings"
)

// IsDebuggerAttached returns true if the program is running under a debugger
func IsDebuggerAttached() bool {
	// Check if running under VS Code debugger
	if os.Getenv("VSCODE_DEBUG_MODE") != "" {
		return true
	}

	// Check if Delve debugger is attached
	if os.Getenv("DELVE_DEBUGGER") != "" {
		return true
	}

	// Check program name for common debug indicators
	programName := os.Args[0]
	if strings.Contains(programName, "__debug_bin") {
		return true
	}

	// On Windows, a real IsDebuggerPresent check would require
	// golang.org/x/sys/windows (kernel32.dll IsDebuggerPresent);
	// not implemented — Windows debugger detection is unsupported.

	return false
}
