package myip

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// seeIPURL is a var so tests can redirect it to a local httptest.Server.
var seeIPURL = "https://ipv4.seeip.org/jsonip"

// defaultTimeout bounds how long SeeIp waits for the external service, so an
// unreachable or slow internet connection can't block the caller forever.
const defaultTimeout = 5 * time.Second

// SeeIp returns the caller's public IPv4 address as seen by an external
// service. It returns an error rather than panicking if the service is
// unreachable, times out, or returns an unexpected response.
func SeeIp(ctx context.Context) (string, error) {
	return SeeIpWithClient(ctx, http.DefaultClient)
}

// SeeIpWithClient is like SeeIp but takes an explicit *http.Client, so tests
// can inject one pointed at a local httptest.Server.
func SeeIpWithClient(ctx context.Context, client *http.Client) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, seeIPURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}

	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching public IP: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from %s", res.StatusCode, seeIPURL)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var data struct {
		Ip string `json:"ip"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	return data.Ip, nil
}
