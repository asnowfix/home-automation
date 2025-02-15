package main

import (
	"hlog"
	"internal/myzone"
)

func main() {
	log := hlog.Logger
	myzone.MyGcpZone(log)
}
