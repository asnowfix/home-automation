package sfr

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
)

var BoxIp = net.ParseIP("192.168.1.1")

var username string = os.Getenv("SFR_USERNAME")

var password string = os.Getenv("SFR_PASSWORD")

func ListDevices() error {
	_, _ = getToken()
	return nil
}

type GetTokenResponse struct {
	XMLName xml.Name `xml:"rsp"`
	Stat    string   `xml:"stat,attr"`
	Version string   `xml:"version,attr"`
	Auth    Auth
}

type Auth struct {
	XMLName xml.Name `xml:"auth"`
	Token   string   `xml:"token,attr"`
	Method  string   `xml:"method,attr"`
}

func getToken() (*string, error) {
	values := url.Values{}
	values.Add("method", "auth.getToken")
	u := &url.URL{
		Scheme:   "http",
		Host:     BoxIp.String(),
		Path:     "/api/1.0/",
		RawQuery: values.Encode(),
		User:     url.UserPassword(username, password),
	}
	// log.Default().Printf("Calling url: %v", u)

	if xmlBytes, err := getXML(u.String()); err != nil {
		log.Default().Printf("Failed to get XML: %v", err)
		return nil, err
	} else {
		log.Default().Printf("Result (Raw): %v", string(xmlBytes))
		var result GetTokenResponse
		if err := xml.Unmarshal(xmlBytes, &result); err != nil {
			return nil, err
		}
		log.Default().Printf("Token: %v", result.Auth.Token)
		log.Default().Printf("Method: %v", result.Auth.Method)
		return &result.Auth.Token, nil
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
