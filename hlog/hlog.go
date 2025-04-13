package hlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Logger logr.Logger

func Init(verbose bool) {
	debugInit("Initializing logger")

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	zerologr.NameFieldName = "logger"
	zerologr.NameSeparator = "/"

	var w io.Writer
	isConsole := IsConsole()
	debugInit(fmt.Sprintf("IsConsole: %v", isConsole))

	if isConsole {
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

	if isConsole {
		zl = zl.Output(zerolog.ConsoleWriter{
			Out:     w,
			NoColor: !isColorTerminal(),
		})
	}

	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	zl = zl.With().Caller().Timestamp().Logger()
	Logger = zerologr.New(&zl)
	Logger.Info("Initialized", "verbose", verbose)

	debugInit("Logger initialization complete")
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

	return IsConsole()
}

func logWriter() (io.Writer, error) {
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
