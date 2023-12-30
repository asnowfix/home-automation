package sfr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
)

var boxIp = net.ParseIP("192.168.1.1")

var username string = os.Getenv("SFR_USERNAME")

var password string = os.Getenv("SFR_PASSWORD")

func ListDevices() ([]byte, error) {
	th, method, err := getToken()
	if err != nil {
		log.Default().Println(err)
		return nil, err
	}
	// tb, err := hex.DecodeString(th)
	// if err != nil {
	// 	log.Default().Println(err)
	// 	return nil, err
	// }
	tb := []byte(th)
	if method == "passwd" || method == "all" {
		uh, err := doHash(username, tb)
		if err != nil {
			log.Default().Println(err)
			return nil, err
		}
		ph, err := doHash(password, tb)
		if err != nil {
			log.Default().Println(err)
			return nil, err
		}
		_, _, _ = checkToken(th, uh+ph)
	}
	return nil, nil
}

func doHash(value string, tb []byte) (string, error) {
	// Fixed size checksum
	// hb := sha256.Sum256([]byte(value))
	// hh := hex.EncodeToString(hb[0:32])

	// Streaming checksum
	h := sha256.New()
	// h := sha3.New256()
	h.Write([]byte(value))
	hh := hex.EncodeToString(h.Sum(nil))

	log.Default().Printf("SHA256(data: %s): %s (len: %v)", value, hh, len(hh))

	// create a new HMAC by defining the hash type and the key
	hmac := hmac.New( /*sha3.New256*/ sha256.New, tb)

	// compute the HMAC
	if _, err := hmac.Write([]byte(hh)); err != nil {
		log.Default().Println(err)
		return "", err
	}
	dataHmac := hmac.Sum(nil)

	hmacHex := hex.EncodeToString(dataHmac)
	secretHex := hex.EncodeToString(tb)

	log.Default().Printf("HMAC_SHA256(key: %s, data: %s): %s (len: %v)", secretHex, string(value), hmacHex, len(hmacHex))
	return hmacHex, nil
}

type AuthResponse struct {
	XMLName xml.Name `xml:"rsp"`
	Status  string   `xml:"stat,attr"`
	Version string   `xml:"version,attr"`
	Auth    Auth
	Error   Error
}

type Auth struct {
	XMLName xml.Name `xml:"auth"`
	Token   string   `xml:"token,attr"`
	Method  string   `xml:"method,attr"`
}

type Error struct {
	XMLName xml.Name `xml:"err"`
	Code    string   `xml:"code,attr"`
	Message string   `xml:"msg,attr"`
}

func getToken() (string, string, error) {
	values := url.Values{}
	values.Add("method", "auth.getToken")
	return authToken(&values)
}

func checkToken(token string, hash string) (string, string, error) {
	values := url.Values{}
	values.Add("method", "auth.checkToken")
	values.Add("token", token)
	values.Add("hash", hash)
	return authToken(&values)
}

func authToken(values *url.Values) (string, string, error) {
	u := &url.URL{
		Scheme:   "http",
		Host:     boxIp.String(),
		Path:     "/api/1.0/",
		RawQuery: values.Encode(),
		// User:     url.UserPassword(username, password),
	}
	log.Default().Printf("Calling url: %v", u)

	if xmlBytes, err := getXML(u.String()); err != nil {
		log.Default().Printf("Failed to get XML: %v", err)
		return "", "", err
	} else {
		log.Default().Printf("Result (Raw): %v", string(xmlBytes))
		var result AuthResponse
		if err := xml.Unmarshal(xmlBytes, &result); err != nil {
			return "", "", err
		}
		if result.Status == "ok" {
			log.Default().Printf("Token: %v", result.Auth.Token)
			log.Default().Printf("Method: %v", result.Auth.Method)
			return result.Auth.Token, result.Auth.Method, nil
		} else {
			log.Default().Printf("Err Code: %v", result.Error.Code)
			log.Default().Printf("Err Msg: %v", result.Error.Message)
			return "", "", fmt.Errorf("%v (%v)", result.Error.Message, result.Error.Code)
		}
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
