package log

import (
	"io"
	"log"
)

var Verbose bool

func Init() {
	if !Verbose {
		log.Default().SetOutput(io.Discard)
	} else {
		// File name & Line number in logs
		log.SetFlags(log.LstdFlags | log.Llongfile)
		log.Default().Print("Turning on logging")
	}
}
