package hlog

import (
	"os"

	"github.com/go-logr/logr"

	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog"
)

var Logger logr.Logger

func Init(verbose bool) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	zerologr.NameFieldName = "logger"
	zerologr.NameSeparator = "/"
	zerologr.SetMaxV(int(zerolog.InfoLevel))

	zl := zerolog.New(os.Stderr)

	nocolor := false
	term, defined := os.LookupEnv("TERM")
	if term == "dumb" || !defined {
		nocolor = true
	}

	zl = zl.Output(zerolog.ConsoleWriter{
		Out:     os.Stderr,
		NoColor: nocolor,
	}) // pretty print
	zl = zl.With().Caller().Timestamp().Logger()
	if verbose {
		zl.Level(zerolog.DebugLevel)
	} else {
		zl.Level(zerolog.InfoLevel)
	}
	Logger = zerologr.New(&zl)
	Logger.Info("Initialized", "verbose", verbose, "TERM", os.Getenv("TERM"))
}
