package main

import (
	"internal/myip"

	"github.com/rs/zerolog/log"
)

func main() {
	ip := myip.SeeIp()
	log.Info().Msgf("IPv4: %v", ip)
}
