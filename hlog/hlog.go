package hlog

import (
	"os"

	"github.com/go-logr/logr"

	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog"
)

var Verbose bool

// var Log logr.Logger

func Init() logr.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	zerologr.NameFieldName = "logger"
	zerologr.NameSeparator = "/"
	zerologr.SetMaxV(1)

	zl := zerolog.New(os.Stderr)
	zl = zl.Output(zerolog.ConsoleWriter{Out: os.Stderr}) // pretty print
	zl = zl.With().Caller().Timestamp().Logger()
	zl.Level(zerolog.DebugLevel)
	var log logr.Logger = zerologr.New(&zl)

	log.Info("Turning on logging")

	// if !Verbose {
	// 	log.Default().SetOutput(io.Discard)
	// } else {
	// 	// File name & Line number in logs
	// 	log.SetFlags(log.LstdFlags | log.Llongfile)
	// 	log.Info("Turning on logging")
	// }

	return log
}
