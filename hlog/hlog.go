package hlog

import (
	"io"
	"os"

	"github.com/go-logr/logr"

	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog"
)

var Logger logr.Logger

func Init(verbose bool) {
	initLogger(os.Stderr, verbose, true) // true for console output
}

func InitWithWriter(verbose bool, w io.Writer) {
	initLogger(w, verbose, false) // false for plain output (no console formatting)
}

func initLogger(w io.Writer, verbose bool, useConsole bool) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	zerologr.NameFieldName = "logger"
	zerologr.NameSeparator = "/"
	zerologr.SetMaxV(int(zerolog.InfoLevel))

	zl := zerolog.New(w)

	if useConsole {
		nocolor := false
		term, defined := os.LookupEnv("TERM")
		if term == "dumb" || !defined {
			nocolor = true
		}

		zl = zl.Output(zerolog.ConsoleWriter{
			Out:     w,
			NoColor: nocolor,
		})
	}

	zl = zl.With().Caller().Timestamp().Logger()
	if verbose {
		zl.Level(zerolog.DebugLevel)
	} else {
		zl.Level(zerolog.InfoLevel)
	}
	Logger = zerologr.New(&zl)
	Logger.Info("Initialized", "verbose", verbose)
}
