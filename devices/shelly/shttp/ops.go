package http

import (
	"bytes"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"

	"github.com/go-logr/logr"
)

var registrar types.MethodsRegistrar

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	// setup logger
	log = l
	log.Info("Init package", reflect.TypeOf(empty{}).PkgPath())

	// register methods
	registrar = r
	registrar.RegisterMethodHandler("HTTP", "GET", types.MethodHandler{
		Allocate:   func() any { return new(Response) },
		HttpMethod: http.MethodGet,
	})
	registrar.RegisterMethodHandler("HTTP", "POST", types.MethodHandler{
		Allocate:   func() any { return new(Response) },
		HttpMethod: http.MethodPost,
	})

	// register channel
	registrar.RegisterDeviceCaller(types.ChannelHttp, types.DeviceCaller(httpChannel.callE))
}

type HttpChannel struct {
}

var httpChannel HttpChannel

func (ch *HttpChannel) callE(device types.Device, verb types.MethodHandler, out any, params any) (any, error) {
	var res *http.Response
	var err error

	switch verb.HttpMethod {
	case http.MethodGet:
		res, err = ch.getE(device.Ipv4(), verb.Method, params)
	default:
		res, err = ch.postE(device.Ipv4(), http.MethodPost, verb.Method, params)
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

func (ch *HttpChannel) getE(ip net.IP, cmd string, params any) (*http.Response, error) {

	values := url.Values{}

	qs := ""
	if params != nil {
		qp, ok := params.(map[string]interface{})
		if ok {
			for key, value := range qp {
				s, err := json.Marshal(value)
				if err == nil {
					values.Add(key, string(s))
				}
			}
		} else {
			err := fmt.Errorf("%s support query parameters only (got %v)", http.MethodGet, reflect.TypeOf(params))
			log.Error(err, "Params error error")
			return nil, err
		}
		qs = fmt.Sprintf("?%s", values.Encode())
	}

	requestURL := fmt.Sprintf("http://%s/rpc/%s%s", ip, cmd, qs)
	log.Info("Calling", "method", http.MethodGet, "url", requestURL)

	res, err := http.Get(requestURL)
	if err != nil {
		log.Error(err, "HTTP GET error")
		return nil, err
	}
	log.Info("status code", "code", res.StatusCode)

	return res, err
}

func (ch *HttpChannel) postE(ip net.IP, hm string, cmd string, params any) (*http.Response, error) {

	var payload struct {
		Id     uint   `json:"id"`
		Method string `json:"method"`
		Params struct {
			Config any `json:"config"`
		} `json:"params"`
	}

	payload.Id = 0
	payload.Method = cmd
	payload.Params.Config = params

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	requestURL := fmt.Sprintf("http://%s/rpc", ip)
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
	log.Info("status code", "code", res.StatusCode)

	return res, err
}
