package myzone

import (
	"context"

	"github.com/rs/zerolog/log"
	dns "google.golang.org/api/dns/v2"
)

func MyGcpZone() {
	ctx := context.Background()
	dnsService, err := dns.NewService(ctx)
	if err != nil {
		log.Fatal().AnErr("Connecting to Google Cloud DNS", err)
	}
	resp, err := dnsService.ManagedZones.List("homeautomation-402816", "none").Do()
	log.Info().Msgf("ManagedZone: %v", resp.ManagedZones)
}
