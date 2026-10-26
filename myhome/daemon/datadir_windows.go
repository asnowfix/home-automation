//go:build windows
// +build windows

package daemon

import (
	"os"
	"path/filepath"
)

// serviceDataDir returns the directory the Windows service should use as its
// working directory, mirroring the ProgramData root already used for service
// logs (see hlog/writer_windows.go getLogDir()).
func serviceDataDir() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = filepath.Join(filepath.VolumeName(os.Getenv("SystemDrive")), "ProgramData")
	}
	return filepath.Join(programData, "MyHome")
}
