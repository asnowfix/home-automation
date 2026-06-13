package beem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/go-logr/logr"
)

// loginURL and summaryURL are vars so tests can redirect to a local httptest.Server.
var (
	loginURL   = "https://api-x.beem.energy/beemapp/user/login"
	summaryURL = "https://api-x.beem.energy/beemapp/box/summary"
)

const (
	// tokenRefreshMargin is how early before expiry we proactively re-authenticate.
	tokenRefreshMargin = 60 * time.Second
)

// loginResponse is the JSON payload returned by the Beem login endpoint.
type loginResponse struct {
	AccessToken string  `json:"accessToken"`
	ExpiresIn   float64 `json:"expiresIn"` // seconds until expiry
}

// summaryResponse is the JSON payload returned by the Beem summary endpoint.
type summaryResponse struct {
	// Instantaneous solar production in watts.
	InstantPower float64 `json:"instantPower"`
	// Energy produced today in watt-hours.
	DayEnergy float64 `json:"dayEnergy"`
	// Energy produced this month in watt-hours.
	MonthEnergy float64 `json:"monthEnergy"`
}

// Client authenticates against the Beem Energy REST API and polls production data.
type Client struct {
	cfg        ClientConfig
	http       http.Client
	token      string
	tokenExpAt time.Time
	log        logr.Logger
}

// NewClient returns a new Beem API client using the supplied configuration.
func NewClient(cfg ClientConfig) *Client {
	return &Client{
		cfg: cfg,
		log: hlog.GetLogger("pkg/beem"),
	}
}

// login authenticates with the Beem API and stores the returned access token.
func (c *Client) login(ctx context.Context) error {
	c.log.Info("Logging in to Beem Energy API", "email", c.cfg.Email)

	body, err := json.Marshal(map[string]string{
		"email":    c.cfg.Email,
		"password": c.cfg.Password,
	})
	if err != nil {
		return fmt.Errorf("beem: marshal login request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("beem: create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("beem: login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("beem: login failed with status %d: %s", resp.StatusCode, string(data))
	}

	var lr loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return fmt.Errorf("beem: decode login response: %w", err)
	}
	if lr.AccessToken == "" {
		return fmt.Errorf("beem: login response missing accessToken")
	}

	c.token = lr.AccessToken
	c.tokenExpAt = time.Now().Add(time.Duration(lr.ExpiresIn) * time.Second)
	c.log.Info("Beem login succeeded", "expires_at", c.tokenExpAt)
	return nil
}

// refreshIfNeeded calls login when the token has expired or will expire within the refresh margin.
func (c *Client) refreshIfNeeded(ctx context.Context) error {
	if c.token == "" || time.Until(c.tokenExpAt) < tokenRefreshMargin {
		return c.login(ctx)
	}
	return nil
}

// PollSummary fetches the current solar production summary from the Beem API.
// On a 401 response it re-authenticates and retries once.
func (c *Client) PollSummary(ctx context.Context) (PowerSample, error) {
	if err := c.refreshIfNeeded(ctx); err != nil {
		return PowerSample{}, err
	}

	sample, status, err := c.fetchSummary(ctx)
	if err == nil {
		return sample, nil
	}

	if status == http.StatusUnauthorized {
		c.log.Info("Received 401 from Beem API, re-authenticating")
		if loginErr := c.login(ctx); loginErr != nil {
			return PowerSample{}, loginErr
		}
		sample, _, err = c.fetchSummary(ctx)
	}
	return sample, err
}

// fetchSummary performs the actual GET /beemapp/box/summary request.
// It returns the parsed sample, the HTTP status code, and any error.
func (c *Client) fetchSummary(ctx context.Context) (PowerSample, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, summaryURL, nil)
	if err != nil {
		return PowerSample{}, 0, fmt.Errorf("beem: create summary request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return PowerSample{}, 0, fmt.Errorf("beem: summary request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return PowerSample{}, http.StatusUnauthorized, fmt.Errorf("beem: unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return PowerSample{}, resp.StatusCode, fmt.Errorf("beem: summary failed with status %d: %s", resp.StatusCode, string(data))
	}

	var sr summaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return PowerSample{}, resp.StatusCode, fmt.Errorf("beem: decode summary response: %w", err)
	}

	sample := PowerSample{
		SolarW:    sr.InstantPower,
		DailyWh:   sr.DayEnergy,
		MonthlyWh: sr.MonthEnergy,
		Source:    "rest",
		TS:        time.Now().UTC(),
	}
	c.log.V(1).Info("Beem summary fetched", "solar_w", sample.SolarW, "daily_wh", sample.DailyWh, "monthly_wh", sample.MonthlyWh)
	return sample, resp.StatusCode, nil
}
