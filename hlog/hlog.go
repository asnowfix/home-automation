package hlog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zerologr"
	"github.com/kardianos/service"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Logger logr.Logger

func LogToStderr() bool {
	return os.Getenv("MYHOME_LOG") == "stderr"
}

func Init(verbose bool) {
	InitWithLevel(verbose, false, zerolog.ErrorLevel)
}

func InitWithDebug(verbose bool, debug bool) {
	InitWithLevel(verbose, debug, zerolog.ErrorLevel)
}

// InitForDaemon initializes logging for daemon processes with info level as default (verbose by default)
func InitForDaemon(verbose bool) {
	InitWithLevel(verbose, false, zerolog.InfoLevel)
}

// InitWithLevel initializes logging with a specific default level
func InitWithLevel(verbose bool, debug bool, defaultLevel zerolog.Level) {
	debugInit("Initializing logger")

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	zerologr.NameFieldName = "logger"
	zerologr.NameSeparator = "/"

	var w io.Writer

	logToStderr := LogToStderr()
	isTerminal := IsTerminal()

	if logToStderr || isTerminal {
		w = os.Stderr
		debugInit("Using stderr for logging")
	} else {
		var err error
		w, err = logWriter()
		if err != nil {
			debugInit(fmt.Sprintf("Failed to create log writer: %v", err))
			panic(err)
		}
		debugInit("Created file log writer")
	}

	zl := zerolog.New(w)

	if isTerminal {
		zl = zl.Output(zerolog.ConsoleWriter{
			Out:        w,
			NoColor:    !isColorTerminal(),
			TimeFormat: time.RFC3339,
		})
	}

	// Determine log level
	level := parseLogLevel(verbose, debug, defaultLevel)
	zerolog.SetGlobalLevel(level)
	zl = zl.Level(level)

	zl = zl.With().Caller().Timestamp().Logger()
	Logger = zerologr.New(&zl)
	Logger.Info("Initialized", "level", level.String(), "verbose", verbose, "debug", debug)

	debugInit("Logger initialization complete")
}

// parseLogLevel converts verbose and debug flags to zerolog level
func parseLogLevel(verbose bool, debug bool, defaultLevel zerolog.Level) zerolog.Level {
	// Auto-detect VSCode debugger and force debug level
	if isRunningUnderDebugger() {
		debugInit("VSCode debugger detected, forcing debug log level")
		return zerolog.DebugLevel
	}
	
	// Handle debug flag (--debug = debug level, shows V(1) logs)
	if debug {
		return zerolog.DebugLevel
	}
	
	// Handle verbose flag (--verbose = info level)
	if verbose {
		return zerolog.InfoLevel
	}
	
	// Use the provided default level if nothing specified
	return defaultLevel
}

// isRunningUnderDebugger detects if the process is running under a debugger (like VSCode)
func isRunningUnderDebugger() bool {
	// Check for common debugger environment variables
	if os.Getenv("DELVE_DEBUGGER") != "" {
		return true
	}
	
	// Check if MYHOME_LOG is set to stderr (common in VSCode launch configs)
	if LogToStderr() {
		return true
	}
	
	// Check for VSCode-specific environment variables
	if os.Getenv("VSCODE_PID") != "" || os.Getenv("VSCODE_IPC_HOOK") != "" {
		return true
	}
	
	return false
}

func isColorTerminal() bool {
	// Check if TERM is set to dumb
	if term := os.Getenv("TERM"); term == "dumb" {
		return false
	}

	// Check if NO_COLOR is set
	if _, exists := os.LookupEnv("NO_COLOR"); exists {
		return false
	}

	// Check if CLICOLOR_FORCE is set
	if _, exists := os.LookupEnv("CLICOLOR_FORCE"); exists {
		return true
	}

	// Disable if CLICOLOR=0
	if os.Getenv("CLICOLOR") == "0" {
		return false
	}

	if term := os.Getenv("TERM"); term != "" {
		// Common color-capable terminals
		if strings.HasSuffix(term, "-256color") ||
			strings.HasSuffix(term, "-color") ||
			strings.HasPrefix(term, "xterm") ||
			strings.HasPrefix(term, "screen") ||
			strings.HasPrefix(term, "vt100") ||
			strings.HasPrefix(term, "ansi") {
			return true
		}
	}

	return IsTerminal()
}

func logWriter() (io.Writer, error) {
	// When running under VSCode debugger, use stderr
	if LogToStderr() {
		debugInit("VSCode debug session detected, using stderr for logging")
		return os.Stderr, nil
	}

	if service.Interactive() {
		debugInit("Running in interactive mode, using stderr for logging")
		return os.Stderr, nil
	}

	// Check if running under systemd (JOURNAL_STREAM or INVOCATION_ID are set)
	if os.Getenv("JOURNAL_STREAM") != "" || os.Getenv("INVOCATION_ID") != "" {
		debugInit("Running under systemd, using stderr for journald")
		return os.Stderr, nil
	}

	logDir := getLogDir()
	debugInit(fmt.Sprintf("Creating log directory: %s", logDir))

	if err := os.MkdirAll(logDir, 0755); err != nil {
		debugInit(fmt.Sprintf("Failed to create log directory: %v", err))
		return nil, fmt.Errorf("failed to create log directory: %v", err)
	}

	logPath := filepath.Join(logDir, "myhome.log")
	debugInit(fmt.Sprintf("Log file path: %s", logPath))

	// Setup rotating logger
	logger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10, // megabytes
		MaxBackups: 5,  // number of backups
		MaxAge:     28, // days
		Compress:   true,
	}

	return logger, nil
}

// GetLogger returns a logger for the given package name
// This is a simple approach that just adds the package name as context
func GetLogger(packageName string) logr.Logger {
	return Logger.WithName(packageName)
}

// GetCallerLogger automatically determines the package name from the caller
// and returns a logger with that package name as context
func GetCallerLogger() logr.Logger {
	// Get caller info (skip 1 frame to get the actual caller)
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		return Logger.WithName("unknown")
	}
	
	// Extract package name from file path
	// e.g., "/path/to/pkg/shelly/script/main.go" -> "pkg/shelly/script"
	packageName := extractPackageName(file)
	return Logger.WithName(packageName)
}

// extractPackageName extracts a reasonable package name from a file path
func extractPackageName(filePath string) string {
	// Find the last occurrence of a known root (like "pkg/", "internal/", "cmd/")
	parts := strings.Split(filePath, "/")
	
	// Look for common Go project structure markers
	for i, part := range parts {
		if part == "pkg" || part == "internal" || part == "cmd" || part == "myhome" {
			// Take everything from this marker to the directory containing the file
			if i+1 < len(parts) {
				// Join from the marker to the directory (exclude filename)
				packageParts := parts[i : len(parts)-1]
				return strings.Join(packageParts, "/")
			}
		}
	}
	
	// Fallback: use the directory name
	dir := filepath.Dir(filePath)
	return filepath.Base(dir)
}

// IsContextCancellation checks if an error is due to context cancellation
func IsContextCancellation(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// ErrorIfNotCanceled logs an error only if it's not due to context cancellation
func ErrorIfNotCanceled(log logr.Logger, err error, msg string, keysAndValues ...interface{}) {
	if err != nil && !IsContextCancellation(err) {
		log.Error(err, msg, keysAndValues...)
	}
}

// LogContextDone logs context cancellation appropriately (as info, not error)
func LogContextDone(ctx context.Context, log logr.Logger, msg string, keysAndValues ...interface{}) {
	if ctx.Err() != nil {
		log.Info(msg+" (context done)", keysAndValues...)
	}
}
