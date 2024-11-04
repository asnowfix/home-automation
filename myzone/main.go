package main

import (
	"hlog"
	"internal/myzone"
)

func main() {
	log := hlog.Init()
	myzone.MyGcpZone(log)
}
