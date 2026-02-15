package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"pkg/shelly/types"
	"strconv"
	"time"

	"github.com/go-logr/logr"
)

// <https://shelly-api-docs.shelly.cloud/gen2/General/RPCChannels#http>

type HttpChannel struct {
}

var httpChannel HttpChannel

func (ch *HttpChannel) callE(ctx context.Context, device types.Device, verb types.MethodHandler, out any, params any) (any, error) {
	var res *http.Response
	var err error
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	log = log.WithName("shelly.HttpChannel")
	ctx = logr.NewContext(ctx, log)

	switch verb.HttpMethod {
	case http.MethodGet:
		res, err = ch.getE(ctx, device.Host(), verb.Method, params)
	default:
		res, err = ch.postE(ctx, device.Host(), http.MethodPost, verb.Method, params)
	}

	if err != nil {
		log.Error(err, "HTTP error - clearing device host to fallback to MQTT", "device_id", device.Id())
		device.ClearHost()
		return nil, err
	}

	err = json.NewDecoder(res.Body).Decode(&out)
	if err != nil {
		log.Error(err, "HTTP error decoding response", "device_id", device.Id())
		return nil, err
	}

	return out, nil

}

func (ch *HttpChannel) getE(ctx context.Context, host string, cmd string, params any) (*http.Response, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	log = log.WithName("shelly.HttpChannel")
	ctx = logr.NewContext(ctx, log)

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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		log.Error(err, "error creating HTTP request")
		return nil, err
	}

	res, err := doWithRetry(ctx, http.DefaultClient, req, 4, 200*time.Millisecond, 3*time.Second, log)
	if err != nil {
		log.Error(err, "HTTP GET error")
		return nil, err
	}

	if res.StatusCode >= 400 {
		err = fmt.Errorf("http client error %d (%s)", res.StatusCode, res.Status)
		return nil, err
	}

	log.Info("status code", "code", res.StatusCode)

	return res, err
}

func (ch *HttpChannel) postE(ctx context.Context, host string, hm string, cmd string, params any) (*http.Response, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	log = log.WithName("shelly.HttpChannel")
	ctx = logr.NewContext(ctx, log)

	var requestURL string
	var jsonData []byte

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
		ip := net.ParseIP(host)
		if ip.To4() == nil {
			// v6
			host = fmt.Sprintf("[%s]", host)
		}
		requestURL = fmt.Sprintf("http://%s/rpc/%s", host, cmd)
		if params != nil {
			jsonData, err = json.Marshal(params)
			if err != nil {
				return nil, err
			}
		}
	}

	log.Info("Preparing", "method", hm, "url", requestURL, "body", string(jsonData))
	req, err := http.NewRequestWithContext(ctx, hm, requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Error(err, "error creating HTTP request")
		return nil, err
	}

	// q := req.URL.Query()
	// q.Add("api_key", "key_from_environment_or_flag")
	// q.Add("another_thing", "foo & bar")
	// req.URL.RawQuery = q.Encode()

	req.Header.Add("Content-Type", "application/json")

	// Ensure we can retry by replaying the body
	if len(jsonData) > 0 {
		bodyCopy := append([]byte(nil), jsonData...)
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyCopy)), nil
		}
	}

	log.Info("Calling", "method", hm, "url", requestURL)
	res, err := doWithRetry(ctx, http.DefaultClient, req, 4, 200*time.Millisecond, 3*time.Second, log)
	if err != nil {
		log.Error(err, "HTTP error")
		return nil, err
	}

	if res.StatusCode >= 400 {
		err = fmt.Errorf("http client error %d (%s)", res.StatusCode, res.Status)
		return nil, err
	}

	log.Info("status code", "code", res.StatusCode)

	return res, err
}

// doWithRetry performs an HTTP request with simple exponential backoff retries.
// It retries on transport errors, 5xx, and 429, honoring Retry-After when present.
func doWithRetry(ctx context.Context, client *http.Client, req *http.Request, maxRetries int, baseDelay, maxDelay time.Duration, log logr.Logger) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}

	backoff := baseDelay
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 5 * time.Second
	}

	attempt := 0
	for {
		// rewind body if needed
		if attempt > 0 && req.Body != nil {
			if req.GetBody != nil {
				b, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				req.Body = b
			}
		}

		res, err := client.Do(req)
		if err == nil && (res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests) {
			return res, nil
		}

		shouldRetry := false
		var wait time.Duration

		if err != nil {
			shouldRetry = true
		} else {
			if res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests {
				shouldRetry = true
				if ra := res.Header.Get("Retry-After"); ra != "" {
					if secs, e := strconv.Atoi(ra); e == nil {
						wait = time.Duration(secs) * time.Second
					} else if t, e := http.ParseTime(ra); e == nil {
						wait = time.Until(t)
						if wait < 0 {
							wait = 0
						}
					}
				}
			}
			if shouldRetry && res != nil && res.Body != nil {
				_ = res.Body.Close()
			}
		}

		if !shouldRetry || attempt >= maxRetries {
			if err != nil {
				return nil, err
			}
			return res, nil
		}

		if wait == 0 {
			wait = backoff
			if wait > maxDelay {
				wait = maxDelay
			}
		}

		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
			// continue
		}

		backoff *= 2
		if backoff > maxDelay {
			backoff = maxDelay
		}
		attempt++
	}
}
