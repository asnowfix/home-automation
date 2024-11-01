package hlog

import (
	"github.com/go-logr/logr"
	"github.com/iand/logfmtr"
)

var Verbose bool

func Init() logr.Logger {

	// Set options that all loggers will be based on
	opts := logfmtr.DefaultOptions()
	opts.Humanize = true
	opts.AddCaller = true
	logfmtr.UseOptions(opts)

	return logfmtr.NewNamed("hlog")
}
