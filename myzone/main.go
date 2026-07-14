package main

import (
	"os"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myzone"
)

func main() {
	log := hlog.Logger
	if err := myzone.MyGcpZone(log); err != nil {
		log.Error(err, "Failed to get GCP zone")
		os.Exit(1)
	}
}
