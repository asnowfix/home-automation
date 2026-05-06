package main

import (
	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myzone"
)

func main() {
	log := hlog.Logger
	myzone.MyGcpZone(log)
}
