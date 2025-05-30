package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"global"
	"net"
	"net/http"
	"net/url"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

// <https://shelly-api-docs.shelly.cloud/gen2/General/RPCChannels#http>

type HttpChannel struct {
}

var httpChannel HttpChannel

func (ch *HttpChannel) callE(ctx context.Context, device types.Device, verb types.MethodHandler, out any, params any) (any, error) {
	var res *http.Response
	var err error
	log := ctx.Value(global.LogKey).(logr.Logger)

	switch verb.HttpMethod {
	case http.MethodGet:
		res, err = ch.getE(ctx, device.Host(), verb.Method, params)
	default:
		res, err = ch.postE(ctx, device.Host(), http.MethodPost, verb.Method, params)
	}

	if err != nil {
		log.Error(err, "HTTP error")
		return nil, err
	}

	err = json.NewDecoder(res.Body).Decode(&out)
	if err != nil {
		log.Error(err, "HTTP error decoding response")
		return nil, err
	}

	return out, nil

}

func (ch *HttpChannel) getE(ctx context.Context, host string, cmd string, params any) (*http.Response, error) {
	log := ctx.Value(global.LogKey).(logr.Logger)

	values := url.Values{}

	qs := ""
	if params != nil {
		qp, ok := params.(map[string]any)
		if !ok {
			b, err := json.Marshal(params)
			if err != nil {
				return nil, err
			}
			json.Unmarshal(b, &qp)
		}
		for key, value := range qp {
			s, err := json.Marshal(value)
			if err == nil {
				values.Add(key, string(s))
			}
		}
		qs = fmt.Sprintf("?%s", values.Encode())
	}

	ip := net.ParseIP(host)
	if ip.To4() == nil {
		// v6
		host = fmt.Sprintf("[%s]", host)
	}
	requestURL := fmt.Sprintf("http://%s/rpc/%s%s", host, cmd, qs)
	log.Info("Calling", "method", http.MethodGet, "url", requestURL)

	res, err := http.Get(requestURL)
	if err != nil {
		log.Error(err, "HTTP GET error")
		return nil, err
	}

	if res.StatusCode >= 400 {
		err = fmt.Errorf("http error %d (%s)", res.StatusCode, res.Status)
		return nil, err
	}

	log.Info("status code", "code", res.StatusCode)

	return res, err
}

func (ch *HttpChannel) postE(ctx context.Context, host string, hm string, cmd string, params any) (*http.Response, error) {
	log := ctx.Value(global.LogKey).(logr.Logger)

	var requestURL string
	var jsonData []byte
	var err error

	if false {
		var payload struct {
			// Id     uint32   `json:"id"`
			Method string `json:"method"`
			Params any    `json:"params"`
		}
		// payload.Id = 0
		payload.Method = cmd
		payload.Params = params
		jsonData, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		requestURL = fmt.Sprintf("http://%s/rpc", host)
	} else {
		requestURL = fmt.Sprintf("http://%s/rpc/%s", host, cmd)
		if params != nil {
			jsonData, err = json.Marshal(params)
			if err != nil {
				return nil, err
			}
		}
	}

	log.Info("Preparing", "method", hm, "url", requestURL, "body", string(jsonData))
	req, err := http.NewRequest(hm, requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Error(err, "error creating HTTP request")
		return nil, err
	}

	// q := req.URL.Query()
	// q.Add("api_key", "key_from_environment_or_flag")
	// q.Add("another_thing", "foo & bar")
	// req.URL.RawQuery = q.Encode()

	req.Header.Add("Content-Type", "application/json")

	log.Info("Calling", "method", hm, "url", requestURL)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error(err, "HTTP error")
	}

	if res.StatusCode >= 400 {
		err = fmt.Errorf("http error %d (%s)", res.StatusCode, res.Status)
		return nil, err
	}

	log.Info("status code", "code", res.StatusCode)

	return res, err
}
