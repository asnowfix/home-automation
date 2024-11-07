package sfr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"

	"github.com/jackpal/gateway"
)

func boxIp() string {
	ip, _ := gateway.DiscoverGateway()
	return ip.String()
}

var username string = os.Getenv("SFR_USERNAME")

var password string = os.Getenv("SFR_PASSWORD")

var token string = ""

type XmlHost struct {
	XMLName   xml.Name         `xml:"host"`
	Type      string           `xml:"type,attr"`
	Name      string           `xml:"name,attr"`
	Ip        net.IP           `xml:"ip,attr"`
	Mac       net.HardwareAddr `xml:"mac,attr"`
	Interface string           `xml:"iface,attr"`
	Probe     uint             `xml:"probe,attr"`
	Alive     uint             `xml:"alive,attr"`
	Status    string           `xml:"status,attr"`
}

type Response struct {
	XMLName xml.Name `xml:"rsp"`
	Status  string   `xml:"stat,attr"`
	Version string   `xml:"version,attr"`
	Error   *Error
	Auth    *Auth
	Hosts   []*XmlHost `xml:"host"`
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

func renewToken() error {
	t, method, err := getToken()
	if err != nil {
		log.Info("getToken()", err)
		return err
	}
	if method == "passwd" || method == "all" {
		err = checkToken(t)
		if err != nil {
			log.Info("checkToken()", err)
			return err
		}
	}
	token = t
	return nil
}

func getToken() (string, string, error) {
	params := map[string]string{}
	res, err := queryBox("auth.getToken", &params)
	if err != nil {
		log.Info("auth.getToken", err)
		return "", "", err
	}
	auth := res.(*Auth)
	log.Info("Auth", "token", auth.Token, "method", auth.Method)
	return auth.Token, auth.Method, nil
}

func checkToken(token string) error {
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
	_, err = queryBox("auth.checkToken", &params)
	if err != nil {
		log.Info("auth.checkToken", err)
		return err
	}
	log.Info("Valid auth", "token", token)
	return nil
}

func doHash(value string, tb []byte) (string, error) {
	h := sha256.New()
	h.Write([]byte(value))
	hh := hex.EncodeToString(h.Sum(nil))

	log.Info("SHA256", "data", value, "hash", hh, "hash_len", len(hh))

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

	log.Info("HMAC_SHA256", "key", secretHex, "data", string(value), "hmac_hex", hmacHex, "hmac_hex_len", len(hmacHex))
	return hmacHex, nil
}

func queryBox(method string, params *map[string]string) (any, error) {
	values := url.Values{}
	values.Add("method", method)
	for k, v := range *params {
		values.Add(k, v)
	}

	u := &url.URL{
		Scheme:   "http",
		Host:     boxIp(),
		Path:     "/api/1.0/",
		RawQuery: values.Encode(),
	}
	log.Info("Calling", "url", u)

	xmlBytes, err := getXML(u.String())
	if err != nil {
		log.Error(err, "Failed to get XML")
		return nil, err
	}
	log.Info("Result", "raw", string(xmlBytes))
	var res Response
	if err := xml.Unmarshal(xmlBytes, &res); err != nil {
		return nil, err
	}
	if res.Status == "fail" {
		log.Info("Err Code", "code", res.Error.Code, "msg", res.Error.Message)
		return nil, fmt.Errorf("%v (%v)", res.Error.Message, res.Error.Code)
	} else if res.Auth != nil {
		return res.Auth, nil
	} else if len(res.Hosts) > 0 {
		return res.Hosts, nil
	} else {
		return nil, fmt.Errorf("unhandled response (%v)", res)
	}
}

// tweaked from: https://stackoverflow.com/a/42718113/1170664
func getXML(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return []byte{}, fmt.Errorf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []byte{}, fmt.Errorf("Status error: %v", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return []byte{}, fmt.Errorf("Read body: %v", err)
	}

	return data, nil
}
