//go:build windows
// +build windows

package hlog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattn/go-isatty"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

// Debug event logger for initialization
var debugLog *eventlog.Log

func init() {
	var err error
	// Try to install the event source if it doesn't exist
	err = eventlog.InstallAsEventCreate("MyHome", eventlog.Info|eventlog.Warning|eventlog.Error)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to install event source: %v\n", err)
	}

	// Open the event log
	debugLog, err = eventlog.Open("MyHome")
	if err == nil {
		debugLog.Info(1, "hlog package initialized")
	} else {
		fmt.Fprintf(os.Stderr, "Failed to open event log: %v\n", err)
	}
}

func debugInit(msg string) {
	if debugLog != nil {
		debugLog.Info(1, "MyHome#Init: "+msg)
	} else {
		fmt.Fprintf(os.Stderr, "MyHome#Init: %s\n", msg)
	}
}

func IsTerminal() bool {
	// First check if we're running as a service
	isService, err := svc.IsWindowsService()
	if err != nil {
		if debugLog != nil {
			debugLog.Warning(1, fmt.Sprintf("IsWindowsService error: %v", err))
		}
		// If we can't determine service status, assume we're not a service
		// but check terminal capabilities
		return isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	}

	if debugLog != nil {
		debugLog.Info(1, fmt.Sprintf("IsWindowsService: %v", isService))
	}

	// If we're a service, we're definitely not a console
	if isService {
		return false
	}

	// Otherwise check terminal capabilities
	isTerminal := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	if debugLog != nil {
		debugLog.Info(1, fmt.Sprintf("IsTerminal: %v", isTerminal))
	}
	return isTerminal
}

func getLogDir() string {
	// If running as a service, use ProgramData
	isService, err := svc.IsWindowsService()
	if err != nil && debugLog != nil {
		debugLog.Warning(1, fmt.Sprintf("getLogDir IsWindowsService error: %v", err))
	}

	if isService {
		dir := filepath.Join(filepath.VolumeName(os.Getenv("SystemDrive")), "ProgramData", "MyHome", "logs")
		if debugLog != nil {
			debugLog.Info(1, fmt.Sprintf("Service log dir: %s", dir))
		}
		return dir
	}

	// Otherwise use %LOCALAPPDATA%
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		// Fallback if LOCALAPPDATA is not set
		appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	dir := filepath.Join(appData, "MyHome", "logs")
	if debugLog != nil {
		debugLog.Info(1, fmt.Sprintf("Local log dir: %s", dir))
	}
	return dir
}
