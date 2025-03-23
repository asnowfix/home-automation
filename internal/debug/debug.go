package debug

import (
	"os"
	"runtime"
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

	// On Windows, check if debugger is present
	if runtime.GOOS == "windows" {
		// Import "golang.org/x/sys/windows" if needed
		// kernel32 := syscall.NewLazyDLL("kernel32.dll")
		// isDebuggerPresent := kernel32.NewProc("IsDebuggerPresent")
		// ret, _, _ := isDebuggerPresent.Call()
		// return ret != 0
	}

	return false
}
