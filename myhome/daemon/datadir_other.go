//go:build !windows
// +build !windows

package daemon

// serviceDataDir is a no-op on non-Windows platforms: systemd/launchd already
// manage the service's working directory (see the WorkingDirectory=/
// WorkingDirectory handling in the kardianos service templates), so the
// daemon does not need to chdir itself.
func serviceDataDir() string {
	return ""
}
