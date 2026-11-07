package sfr

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/jackpal/gateway"
)

// tokenAttrRE matches a token attribute in a box XML response, the form
// auth.getToken uses to hand back the session token.
var tokenAttrRE = regexp.MustCompile(`(token=")[^"]*(")`)

// redactTokenAttributes hides the value of every token attribute in raw box
// XML before it reaches a log line (#620).
func redactTokenAttributes(raw string) string {
	return tokenAttrRE.ReplaceAllString(raw, `${1}<redacted>${2}`)
}

var (
	boxIp      net.IP
	boxIpMutex sync.Mutex
	log        logr.Logger

	// sfrHTTPClient is used for all SFR box HTTP calls.
	// - Timeout: makes gateway probing fail fast when the router is unreachable
	//   (e.g. running behind a proxy or during a transient network issue) instead
	//   of blocking indefinitely on the default http.Client.
	// - Proxy: honours HTTP_PROXY / HTTPS_PROXY / NO_PROXY env vars so that an
	//   SSH SOCKS tunnel (ssh -D 1080 home-pi) can forward requests to the home LAN
	//   when the daemon is running on a machine without direct LAN access.
	sfrHTTPClient = &http.Client{
		Timeout:   3 * time.Second,
		Transport: http.DefaultTransport,
	}
)

func getBoxIp(ctx context.Context) net.IP {
	boxIpMutex.Lock()
	defer boxIpMutex.Unlock()

	if boxIp != nil {
		return boxIp
	}

	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil
	}
	log = log.WithName("sfr")

	ips, err := gateway.DiscoverGateways()
	if err != nil {
		log.Error(err, "Failed to discover gateway")
		return nil
	}

	// loop on every IP, trying to test the SFR public API
	for _, ip := range ips {
		log.Info("Testing gateway IP", "ip", ip.String())
		info, err := GetLanInfo(ctx, ip)
		if err == nil {
			log.Info("Discovered gateway IP", "ip", ip.String(), "info", info)
			boxIp = ip
			return boxIp
		}
		log.V(1).Info("Skipping potential gateway", "ip", ip, "error", err)
	}

	return boxIp
}

var username string

var password string

var token string

// Init sets the SFR box credentials. Call this once at startup (e.g. from Viper config).
// If not called, or called with empty strings, authentication is skipped.
func Init(u, p string) {
	username = u
	password = p
}

type Response struct {
	XMLName  xml.Name `xml:"rsp"`
	Status   string   `xml:"stat,attr"`
	Version  string   `xml:"version,attr"`
	Error    *Error
	Auth     *Auth
	LanInfo  *LanInfo    `xml:"lan,omitempty"`
	Hosts    *[]*LanHost `xml:"host,omitempty"`
	DnsHosts *[]*DnsHost `xml:"dns,omitempty"`
}

type Error struct {
	XMLName xml.Name `xml:"err"`
	Code    string   `xml:"code,attr"`
	Message string   `xml:"msg,attr"`
}

type Auth struct {
	XMLName xml.Name `xml:"auth"`
	Token   string   `xml:"token,attr"`
	Method  string   `xml:"method,attr"`
}

func renewToken(ip net.IP) error {

	t, method, err := getToken(ip)
	if err != nil {
		log.Info("getToken()", err)
		return err
	}
	if (method == "passwd" || method == "all") && (username != "" || password != "") {
		err = checkToken(ip, t)
		if err != nil {
			log.Info("checkToken()", err)
			return err
		}
	}
	token = t
	return nil
}

func getToken(ip net.IP) (string, string, error) {
	params := map[string]string{}
	res, err := queryBox(ip, "auth.getToken", &params)
	if err != nil {
		log.Info("auth.getToken", err)
		return "", "", err
	}
	auth := res.(*Auth)
	// The token is a session secret; never write it to the journal (#620).
	log.V(1).Info("Auth", "method", auth.Method)
	return auth.Token, auth.Method, nil
}

func checkToken(ip net.IP, token string) error {
	uh, err := doHash(username, []byte(token))
	if err != nil {
		log.Info("doHash()", err)
		return err
	}
	ph, err := doHash(password, []byte(token))
	if err != nil {
		log.Info("doHash()", err)
		return err
	}
	params := map[string]string{
		"token": token,
		"hash":  uh + ph,
	}
	_, err = queryBox(ip, "auth.checkToken", &params)
	if err != nil {
		log.Info("auth.checkToken", err)
		return err
	}
	log.V(1).Info("Valid auth")
	return nil
}

func doHash(value string, tb []byte) (string, error) {
	h := sha256.New()
	h.Write([]byte(value))
	hh := hex.EncodeToString(h.Sum(nil))

	// Only the lengths: the input can be the box admin login or password, and
	// the hash is half of the auth check's secret (#620).
	log.V(2).Info("SHA256", "hash_len", len(hh))

	// create a new HMAC by defining the hash type and the key
	hmac := hmac.New(sha256.New, tb)

	// compute the HMAC
	if _, err := hmac.Write([]byte(hh)); err != nil {
		log.Error(err, "hmac.Write()")
		return "", err
	}
	dataHmac := hmac.Sum(nil)

	hmacHex := hex.EncodeToString(dataHmac)
	secretHex := hex.EncodeToString(tb)

	log.V(2).Info("HMAC_SHA256", "key_len", len(secretHex), "hmac_hex_len", len(hmacHex))
	return hmacHex, nil
}

func queryBox(ip net.IP, method string, params *map[string]string) (any, error) {
	values := url.Values{}
	values.Add("method", method)
	for k, v := range *params {
		values.Add(k, v)
	}

	u := &url.URL{
		Scheme:   "http",
		Host:     ip.String(),
		Path:     "/api/1.0/",
		RawQuery: values.Encode(),
	}
	// auth.checkToken carries the session token and its hash in the query
	// string, so redact the secret parameters before logging (#620).
	redacted := *u
	q := redacted.Query()
	for _, k := range []string{"token", "hash"} {
		if q.Has(k) {
			q.Set(k, "<redacted>")
		}
	}
	redacted.RawQuery = q.Encode()
	log.Info("Calling", "url", redacted.String())

	xmlBytes, err := getXML(u.String())
	if err != nil {
		log.Error(err, "Failed to get XML")
		return nil, err
	}
	// Some responses embed the session token as an attribute; keep the raw
	// body behind V(1) and hide the attribute value (#620).
	log.V(1).Info("Result", "raw", redactTokenAttributes(string(xmlBytes)))

	var res Response
	if err := xml.Unmarshal(xmlBytes, &res); err != nil {
		return nil, err
	}
	if res.Status == "fail" {
		log.Info("Err Code", "code", res.Error.Code, "msg", res.Error.Message)
		return nil, fmt.Errorf("%v (%v)", res.Error.Message, res.Error.Code)
	} else if res.Auth != nil {
		return res.Auth, nil
	} else if res.LanInfo != nil {
		return res.LanInfo, nil
	} else if res.Hosts != nil && len(*res.Hosts) > 0 {
		return res.Hosts, nil
	} else if res.DnsHosts != nil && len(*res.DnsHosts) > 0 {
		return res.DnsHosts, nil
	} else {
		return nil, fmt.Errorf("unhandled response (%v)", string(xmlBytes))
	}
}

// tweaked from: https://stackoverflow.com/a/42718113/1170664
func getXML(rawURL string) ([]byte, error) {
	resp, err := sfrHTTPClient.Get(rawURL)
	if err != nil {
		// *url.Error embeds the full request URL, whose query string carries
		// the session token and hash on auth.checkToken: keep only the cause
		// (#620).
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return []byte{}, fmt.Errorf("GET error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []byte{}, fmt.Errorf("status error: %v", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return []byte{}, fmt.Errorf("read body: %v", err)
	}

	return data, nil
}
