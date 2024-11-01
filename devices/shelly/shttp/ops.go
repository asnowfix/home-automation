package http

import (
	"bytes"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"

	"github.com/go-logr/logr"
)

var registrar types.MethodsRegistrar

var logger logr.Logger

type empty struct{}

func Init(r types.MethodsRegistrar, l logr.Logger) {
	// setup logger
	logger = l
	logger.Info("Init package", reflect.TypeOf(empty{}).PkgPath())

	// register methods
	registrar = r
	registrar.RegisterMethodHandler("HTTP", "GET", types.MethodHandler{
		Allocate:   func() any { return new(Response) },
		HttpQuery:  map[string]string{},
		HttpMethod: http.MethodGet,
	})
	registrar.RegisterMethodHandler("HTTP", "POST", types.MethodHandler{
		Allocate:   func() any { return new(Response) },
		HttpQuery:  map[string]string{},
		HttpMethod: http.MethodPost,
	})

	// register channel
	registrar.RegisterDeviceCaller(types.ChannelHttp, types.DeviceCaller(httpChannel.callE))
}

type HttpChannel struct{}

var httpChannel HttpChannel

func (ch *HttpChannel) callE(device types.Device, verb types.MethodHandler, out any, body any) (any, error) {
	var res *http.Response
	var err error

	switch verb.HttpMethod {
	case http.MethodGet:
		res, err = ch.getE(device.Ipv4(), verb.Method, verb.HttpQuery)
	case http.MethodPost:
		res, err = ch.postE(device.Ipv4(), verb.Method, body)
	default:
		return nil, fmt.Errorf("unhandled HTTP method '%v' for Shelly method '%v'", verb.HttpQuery, verb.Method)
	}

	if err != nil {
		log.Default().Print(err)
		return nil, err
	}

	err = json.NewDecoder(res.Body).Decode(&out)
	if err != nil {
		log.Default().Print(err)
		return nil, err
	}

	return out, nil

}

func (ch *HttpChannel) getE(ip net.IP, cmd string, qp types.QueryParams) (*http.Response, error) {

	values := url.Values{}
	for key, value := range qp {
		values.Add(key, value)
	}
	query := values.Encode()

	requestURL := fmt.Sprintf("http://%s/rpc/%s?%s", ip, cmd, query)
	logger.Info("calling:", "url", requestURL)

	res, err := http.Get(requestURL)
	if err != nil {
		logger.Info("error making http request", "error", err)
		return nil, err
	}
	logger.Info("status code", "code", res.StatusCode)

	return res, err
}

func (ch *HttpChannel) postE(ip net.IP, cmd string, params any) (*http.Response, error) {

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
	logger.Info("Preparing", "method", http.MethodPost, "url", requestURL, "body", string(jsonData))

	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}

	// q := req.URL.Query()
	// q.Add("api_key", "key_from_environment_or_flag")
	// q.Add("another_thing", "foo & bar")
	// req.URL.RawQuery = q.Encode()

	req.Header.Add("Content-Type", "application/json")

	logger.Info("Calling", "method", http.MethodPost, "url", requestURL)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Default().Printf("error making http request: %s", err)
		return nil, err
	}
	logger.Info("status code", "code", res.StatusCode)

	return res, err
}
