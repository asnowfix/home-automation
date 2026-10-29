// Package cloud reads a robot's persistent maps and rooms from the iRobot cloud
// account (#628). The robot does not report its room names (or, until a room
// clean has run, its room ids) over the local protocol; the cloud does.
//
// It is a read-only client. One call to Maps runs the whole chain once:
//
//  1. discovery: GET {Discovery}/v1/app/discover?country_code=CC, which names
//     the Gigya API key, the iRobot API bases and the AWS region;
//  2. Gigya login: POST {Gigya}/accounts.login with the account email and
//     password (plain email+password accounts only: Google/Apple sign-in and
//     two-factor accounts are not supported);
//  3. iRobot login: POST {httpBase}/v1/login/account, which returns temporary
//     AWS credentials;
//  4. GET {httpBaseAuth}/v1/<blid>/pmaps?activeDetails=1, SigV4-signed.
//
// There is no retry: a sync is one login attempt, so a wrong password cannot
// lock the account. Every HTTP call has a timeout.
//
// The flow follows the community client tidalf/irbt (irbt/cloud.py and
// irbt/robot.py); iRobot publishes no documentation for it. The chain was
// verified once against a real account on 2026-10-29: see
// docs/irobot-local-api-research.md.
//
// Secrets: the account password, the Gigya signature and the AWS credentials
// never leave this package, never appear in an error, and are never logged.
// The account's robot list (/v1/user/associations/robots) carries each robot's
// local password, so this package does not call it.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Default endpoints. Fields of Client so tests can point them at httptest servers.
const (
	DefaultDiscovery = "https://disc-prod.iot.irobotapi.com"
	DefaultGigya     = "https://accounts.us1.gigya.com"
	DefaultCountry   = "FR"
	// DefaultAppID is the app id irbt sends to /v1/login/account (irbt/cloud.py).
	DefaultAppID = "IOS-F700B76F-80EE-4AB9-9B02-34B210F3B148"
	// DefaultTimeout bounds each HTTP call (not the whole chain).
	DefaultTimeout = 15 * time.Second
)

// Step names the stage of the chain that failed, for error reports.
type Step string

const (
	StepDiscover Step = "discover"
	StepGigya    Step = "gigya login"
	StepLogin    Step = "irobot login"
	StepMaps     Step = "pmaps"
)

// Error reports which step failed. GigyaCode is Gigya's errorCode (0 when the
// failure is not a Gigya one); HTTPStatus is the HTTP status (0 when no reply).
// It never contains a credential or a token.
type Error struct {
	Step       Step
	HTTPStatus int
	GigyaCode  int
	Msg        string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "irobot cloud: %s failed", e.Step)
	if e.HTTPStatus != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.HTTPStatus)
	}
	if e.GigyaCode != 0 {
		fmt.Fprintf(&b, " (Gigya errorCode %d)", e.GigyaCode)
	}
	if e.Msg != "" {
		b.WriteString(": " + e.Msg)
	}
	return b.String()
}

// Region is a room (or zone) on a cloud map.
type Region struct {
	ID   string // region id, as sent to the robot in a room clean
	Name string // the name given in the iRobot app
	Type string // the cloud's room type (kitchen, bedroom, ...); not the rid/zid of a clean command
}

// Map is one of the robot's persistent maps as the cloud knows it.
type Map struct {
	ID      string // pmap_id, the same id the robot reports locally
	Version string // user_pmapv_id of the cloud's active version; may differ from the robot's
	Name    string
	Regions []Region
}

// Client is an iRobot cloud client. The zero value is not usable: use New.
type Client struct {
	HTTP      *http.Client
	Discovery string // discovery base URL
	Gigya     string // Gigya accounts base URL
	Country   string
	AppID     string
	Now       func() time.Time // SigV4 clock
}

// New returns a client with the production endpoints and DefaultTimeout per call.
func New() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: DefaultTimeout},
		Discovery: DefaultDiscovery,
		Gigya:     DefaultGigya,
		Country:   DefaultCountry,
		AppID:     DefaultAppID,
		Now:       time.Now,
	}
}

type discovery struct {
	Gigya struct {
		APIKey string `json:"api_key"`
	} `json:"gigya"`
	HTTPBase     string `json:"httpBase"`
	HTTPBaseAuth string `json:"httpBaseAuth"`
	AWSRegion    string `json:"awsRegion"`
}

// Maps logs in once and returns the robot's maps with their rooms. A map
// without rooms is returned with an empty Regions.
func (c *Client) Maps(ctx context.Context, email, password, blid string) ([]Map, error) {
	if email == "" || password == "" {
		return nil, errors.New("irobot cloud: account email and password are required")
	}
	if blid == "" {
		return nil, errors.New("irobot cloud: robot id (BLID) is required")
	}
	d, err := c.discover(ctx)
	if err != nil {
		return nil, err
	}
	uid, sig, ts, err := c.gigyaLogin(ctx, d.Gigya.APIKey, email, password)
	if err != nil {
		return nil, err
	}
	creds, err := c.irobotLogin(ctx, d.HTTPBase, uid, sig, ts)
	if err != nil {
		return nil, err
	}
	return c.pmaps(ctx, d, creds, blid)
}

func (c *Client) discover(ctx context.Context) (*discovery, error) {
	u := strings.TrimRight(c.Discovery, "/") + "/v1/app/discover?country_code=" + url.QueryEscape(c.Country)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, &Error{Step: StepDiscover, Msg: err.Error()}
	}
	body, status, err := c.do(req)
	if err != nil {
		return nil, &Error{Step: StepDiscover, Msg: err.Error()}
	}
	if status != http.StatusOK {
		return nil, &Error{Step: StepDiscover, HTTPStatus: status}
	}
	var d discovery
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, &Error{Step: StepDiscover, HTTPStatus: status, Msg: "unexpected reply: " + err.Error()}
	}
	if d.Gigya.APIKey == "" || d.HTTPBase == "" || d.HTTPBaseAuth == "" || d.AWSRegion == "" {
		return nil, &Error{Step: StepDiscover, HTTPStatus: status, Msg: "reply lacks gigya.api_key, httpBase, httpBaseAuth or awsRegion"}
	}
	return &d, nil
}

func (c *Client) gigyaLogin(ctx context.Context, apiKey, email, password string) (uid, sig, ts string, err error) {
	form := url.Values{
		"ApiKey":            {apiKey},
		"ctag":              {"webbridge"},
		"format":            {"json"},
		"loginID":           {email},
		"password":          {password},
		"sessionExpiration": {"-2"},
		"targetEnv":         {"mobile"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Gigya, "/")+"/accounts.login",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", &Error{Step: StepGigya, Msg: err.Error()}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, status, err := c.do(req)
	if err != nil {
		return "", "", "", &Error{Step: StepGigya, Msg: err.Error()}
	}
	var r struct {
		ErrorCode          int    `json:"errorCode"`
		ErrorMessage       string `json:"errorMessage"`
		UID                string `json:"UID"`
		UIDSignature       string `json:"UIDSignature"`
		SignatureTimestamp string `json:"signatureTimestamp"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", "", "", &Error{Step: StepGigya, HTTPStatus: status, Msg: "unexpected reply"}
	}
	if r.ErrorCode != 0 {
		return "", "", "", &Error{Step: StepGigya, HTTPStatus: status, GigyaCode: r.ErrorCode,
			Msg: gigyaHint(r.ErrorCode, r.ErrorMessage)}
	}
	if status != http.StatusOK {
		return "", "", "", &Error{Step: StepGigya, HTTPStatus: status}
	}
	if r.UID == "" || r.UIDSignature == "" || r.SignatureTimestamp == "" {
		return "", "", "", &Error{Step: StepGigya, HTTPStatus: status,
			Msg: "no UID signature in the reply; only plain email+password accounts are supported (not Google/Apple sign-in or two-factor)"}
	}
	return r.UID, r.UIDSignature, r.SignatureTimestamp, nil
}

// gigyaHint turns a Gigya error into advice. Gigya's message is safe to show:
// it describes the error, never the credentials. The codes are Gigya's
// published error codes (not iRobot-specific); none has been observed here.
func gigyaHint(code int, msg string) string {
	switch code {
	case 403042: // invalid loginID or password
		return "invalid email or password (MYHOME_IROBOT_EMAIL / MYHOME_IROBOT_PASSWORD)"
	case 403120: // account temporarily locked out
		return "account temporarily locked: wait before trying again (each sync makes one attempt)"
	case 403101, 403102, 206001, 206002: // two-factor pending, registration/verification pending
		return "the account needs an extra login step (two-factor or verification), which is not supported: only plain email+password accounts work"
	}
	if msg == "" {
		return "login refused; only plain email+password accounts are supported (not Google/Apple sign-in or two-factor)"
	}
	return msg + "; only plain email+password accounts are supported (not Google/Apple sign-in or two-factor)"
}

func (c *Client) irobotLogin(ctx context.Context, httpBase, uid, sig, ts string) (credentials, error) {
	payload, _ := json.Marshal(map[string]string{
		"app_id":                 c.AppID,
		"assume_robot_ownership": "0",
		"signature":              sig,
		"timestamp":              ts,
		"uid":                    uid,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(httpBase, "/")+"/v1/login/account",
		bytes.NewReader(payload))
	if err != nil {
		return credentials{}, &Error{Step: StepLogin, Msg: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	body, status, err := c.do(req)
	if err != nil {
		return credentials{}, &Error{Step: StepLogin, Msg: err.Error()}
	}
	if status != http.StatusOK {
		return credentials{}, &Error{Step: StepLogin, HTTPStatus: status}
	}
	var r struct {
		Credentials struct {
			AccessKeyID  string `json:"AccessKeyId"`
			SecretKey    string `json:"SecretKey"`
			SessionToken string `json:"SessionToken"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return credentials{}, &Error{Step: StepLogin, HTTPStatus: status, Msg: "unexpected reply"}
	}
	cr := r.Credentials
	if cr.AccessKeyID == "" || cr.SecretKey == "" || cr.SessionToken == "" {
		return credentials{}, &Error{Step: StepLogin, HTTPStatus: status, Msg: "no credentials in the reply"}
	}
	return credentials{AccessKeyID: cr.AccessKeyID, SecretKey: cr.SecretKey, SessionToken: cr.SessionToken}, nil
}

type pmapReply struct {
	PmapID       string `json:"pmap_id"`
	UserPmapvID  string `json:"user_pmapv_id"`
	ActiveDetail struct {
		MapHeader struct {
			Name string `json:"name"`
		} `json:"map_header"`
		Regions []struct {
			ID         json.RawMessage `json:"id"` // a string of digits; tolerate a number
			Name       string          `json:"name"`
			RegionType string          `json:"region_type"`
		} `json:"regions"`
	} `json:"active_pmapv_details"`
}

func (c *Client) pmaps(ctx context.Context, d *discovery, creds credentials, blid string) ([]Map, error) {
	u := strings.TrimRight(d.HTTPBaseAuth, "/") + "/v1/" + url.PathEscape(blid) + "/pmaps?activeDetails=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, &Error{Step: StepMaps, Msg: err.Error()}
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	signV4(req, nil, creds, d.AWSRegion, "execute-api", now())
	body, status, err := c.do(req)
	if err != nil {
		return nil, &Error{Step: StepMaps, Msg: err.Error()}
	}
	if status != http.StatusOK {
		return nil, &Error{Step: StepMaps, HTTPStatus: status}
	}
	var replies []pmapReply
	if err := json.Unmarshal(body, &replies); err != nil {
		return nil, &Error{Step: StepMaps, HTTPStatus: status, Msg: "unexpected reply: " + err.Error()}
	}
	maps := make([]Map, 0, len(replies))
	for _, r := range replies {
		if r.PmapID == "" {
			continue
		}
		m := Map{ID: r.PmapID, Version: r.UserPmapvID, Name: r.ActiveDetail.MapHeader.Name, Regions: []Region{}}
		for _, reg := range r.ActiveDetail.Regions {
			id := regionID(reg.ID)
			if id == "" {
				continue
			}
			m.Regions = append(m.Regions, Region{ID: id, Name: reg.Name, Type: reg.RegionType})
		}
		maps = append(maps, m)
	}
	return maps, nil
}

// regionID accepts "17" or 17.
func regionID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// maxReply bounds what is read from any reply.
const maxReply = 4 << 20

func (c *Client) do(req *http.Request) ([]byte, int, error) {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, scrub(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return nil, resp.StatusCode, scrub(err)
	}
	return body, resp.StatusCode, nil
}

// scrub drops the URL from a transport error: *url.Error prints the request
// URL, which is harmless today (no secret is ever in a URL) but keeps the
// rule simple: errors from this package carry no request detail.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return errors.New("timeout")
		}
		return ue.Err
	}
	return err
}
