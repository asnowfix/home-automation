package myzone

import (
	"context"
	"log"
	"os"

	dns "google.golang.org/api/dns/v2"
	"google.golang.org/api/option"
)

func MyGcpZone() error {
	ctx := context.Background()

	// config := &oauth2.Config{
	// 	// RedirectURL:  "http://localhost:8000/auth/google/callback",
	// 	ClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
	// 	ClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
	// 	Scopes:       []string{"https://www.googleapis.com/auth/ndev.clouddns.readonly"},
	// 	Endpoint:     google.Endpoint,
	// }
	// token, err := config.Exchange(ctx)
	// dnsService, err := dns.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx, token)))

	dnsService, err := dns.NewService(ctx, option.WithAPIKey(os.Getenv("GOOGLE_API_KEY")), option.WithScopes(dns.NdevClouddnsReadwriteScope))

	// dnsService, err := dns.NewService(ctx)

	if err != nil {
		panic(err)
	}
	resp, err := dnsService.ManagedZones.List("homeautomation-402816", "global").Do()
	if err != nil {
		panic(err)
	}
	log.Default().Printf("Projects.Get: %v", resp.ManagedZones)
	return nil
}
