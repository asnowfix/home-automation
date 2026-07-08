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

// loginURL, summaryURL and devicesURL are vars so tests can redirect to a local httptest.Server.
var (
	loginURL   = "https://api-x.beem.energy/beemapp/user/login"
	summaryURL = "https://api-x.beem.energy/beemapp/box/summary"
	devicesURL = "https://api-x.beem.energy/beemapp/devices"
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

// boxSummary is one entry of the JSON array returned by the Beem summary
// endpoint: one object per registered Beem box. Field names match the wire
// format, captured from a live response (confirmed against the
// CharlesP44/Beem_Energy Home Assistant integration, since Beem has no
// public API docs). All fields are modeled even though PowerSample only
// surfaces WattHour/TotalDay/TotalMonth today, so a future consumer doesn't
// need another round of wire-format archaeology.
type boxSummary struct {
	BoxID        int       `json:"boxId"`
	SerialNumber string    `json:"serialNumber"`
	Name         string    `json:"name"`
	LastAlive    time.Time `json:"lastAlive"`
	// LastProduction is the timestamp of the last non-zero production sample.
	LastProduction time.Time `json:"lastProduction"`
	// Energy produced this month in watt-hours.
	TotalMonth float64 `json:"totalMonth"`
	// Energy produced today in watt-hours.
	TotalDay float64 `json:"totalDay"`
	// Instantaneous solar production in watts (misleadingly named on the wire).
	WattHour float64 `json:"wattHour"`
	Year     int     `json:"year"`
	Month    int     `json:"month"`
	// LastDbm is the box's last known WiFi signal strength in dBm.
	LastDbm int `json:"lastDbm"`
	// Power is the box's nameplate/peak power rating in watts, not a live reading.
	Power float64 `json:"power"`
	// Weather is present but always null in every capture so far; kept as
	// raw JSON since its populated shape is undocumented.
	Weather json.RawMessage `json:"weather"`
}

// DevicesResponse is the JSON payload returned by GET /beemapp/devices: the
// full device topology for the account. Batteries and EnergySwitches are
// always empty for this project's PnP-kit-only household (no Beem Battery,
// see docs/beem-energy.md) and are kept as raw JSON rather than modeled,
// since their populated shape has never been observed against real hardware.
type DevicesResponse struct {
	BeemBoxes      []BeemBoxDevice   `json:"beemboxes"`
	Batteries      []json.RawMessage `json:"batteries"`
	EnergySwitches []json.RawMessage `json:"energySwitches"`
}

// BeemBoxDevice is one entry of DevicesResponse.BeemBoxes: static box
// metadata and its attached solar panel equipment (as opposed to boxSummary,
// which carries the box's production figures).
type BeemBoxDevice struct {
	ID              int              `json:"id"`
	HouseID         int              `json:"houseId"`
	Name            string           `json:"name"`
	SerialNumber    string           `json:"serialNumber"`
	Power           float64          `json:"power"`
	CreatedAt       time.Time        `json:"createdAt"`
	SolarEquipments []SolarEquipment `json:"solarEquipments"`
}

// SolarEquipment is one panel array attached to a BeemBoxDevice.
type SolarEquipment struct {
	ID              int     `json:"id"`
	Type            string  `json:"type"`
	BoxID           int     `json:"boxId"`
	Orientation     float64 `json:"orientation"`
	Tilt            float64 `json:"tilt"`
	PeakPower       float64 `json:"peakPower"`
	GuaranteeStatus string  `json:"guaranteeStatus"`
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

// fetchSummary performs the actual POST /beemapp/box/summary request.
// The endpoint requires a JSON body of {month, year} for the current period
// and returns a JSON array with one entry per registered Beem box.
// It returns the parsed sample, the HTTP status code, and any error.
func (c *Client) fetchSummary(ctx context.Context) (PowerSample, int, error) {
	now := time.Now()
	body, err := json.Marshal(map[string]int{
		"month": int(now.Month()),
		"year":  now.Year(),
	})
	if err != nil {
		return PowerSample{}, 0, fmt.Errorf("beem: marshal summary request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, summaryURL, bytes.NewReader(body))
	if err != nil {
		return PowerSample{}, 0, fmt.Errorf("beem: create summary request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return PowerSample{}, 0, fmt.Errorf("beem: summary request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return PowerSample{}, http.StatusUnauthorized, fmt.Errorf("beem: unauthorized")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return PowerSample{}, resp.StatusCode, fmt.Errorf("beem: summary failed with status %d: %s", resp.StatusCode, string(data))
	}

	var boxes []boxSummary
	if err := json.NewDecoder(resp.Body).Decode(&boxes); err != nil {
		return PowerSample{}, resp.StatusCode, fmt.Errorf("beem: decode summary response: %w", err)
	}
	if len(boxes) == 0 {
		return PowerSample{}, resp.StatusCode, fmt.Errorf("beem: summary response contained no boxes")
	}

	// Single-box household assumption: with more than one registered Beem
	// box this would need to sum production across boxes, but nothing in
	// this project's setup has more than one.
	box := boxes[0]
	sample := PowerSample{
		SolarW:    box.WattHour,
		DailyWh:   box.TotalDay,
		MonthlyWh: box.TotalMonth,
		Source:    "rest",
		TS:        time.Now().UTC(),
	}
	c.log.V(1).Info("Beem summary fetched", "solar_w", sample.SolarW, "daily_wh", sample.DailyWh, "monthly_wh", sample.MonthlyWh)
	return sample, resp.StatusCode, nil
}

// GetDevices fetches the account's device topology (Beem boxes, batteries,
// energy switches) from the Beem API. On a 401 response it re-authenticates
// and retries once, mirroring PollSummary.
func (c *Client) GetDevices(ctx context.Context) (DevicesResponse, error) {
	if err := c.refreshIfNeeded(ctx); err != nil {
		return DevicesResponse{}, err
	}

	devices, status, err := c.fetchDevices(ctx)
	if err == nil {
		return devices, nil
	}

	if status == http.StatusUnauthorized {
		c.log.Info("Received 401 from Beem API, re-authenticating")
		if loginErr := c.login(ctx); loginErr != nil {
			return DevicesResponse{}, loginErr
		}
		devices, _, err = c.fetchDevices(ctx)
	}
	return devices, err
}

// fetchDevices performs the actual GET /beemapp/devices request.
// It returns the parsed topology, the HTTP status code, and any error.
func (c *Client) fetchDevices(ctx context.Context) (DevicesResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, devicesURL, nil)
	if err != nil {
		return DevicesResponse{}, 0, fmt.Errorf("beem: create devices request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return DevicesResponse{}, 0, fmt.Errorf("beem: devices request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return DevicesResponse{}, http.StatusUnauthorized, fmt.Errorf("beem: unauthorized")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return DevicesResponse{}, resp.StatusCode, fmt.Errorf("beem: devices failed with status %d: %s", resp.StatusCode, string(data))
	}

	var dr DevicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return DevicesResponse{}, resp.StatusCode, fmt.Errorf("beem: decode devices response: %w", err)
	}

	c.log.V(1).Info("Beem devices fetched", "beemboxes", len(dr.BeemBoxes), "batteries", len(dr.Batteries), "energy_switches", len(dr.EnergySwitches))
	return dr, resp.StatusCode, nil
}
