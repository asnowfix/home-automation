package http

import (
	"bytes"
	"devices/shelly"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
)

func init() {
	// shelly.RegisterMethodHandler("Http", "GetStatus", types.MethodHandler{
	// 	Allocate: func() any { return new(Status) },
	// 	HttpQuery: map[string]string{
	// 		"id": "0",
	// 	},
	// 	HttpMethod: http.MethodGet,
	// })

	shelly.RegisterDeviceCaller(shelly.Http, shelly.DeviceCaller(Call))
}

func Call(device *shelly.Device, verb types.MethodHandler, method string, out any, body any) (any, error) {
	var res *http.Response
	var err error

	switch verb.HttpMethod {
	case http.MethodGet:
		res, err = getE(device.Ipv4, method, verb.HttpQuery)
	case http.MethodPost:
		res, err = postE(device.Ipv4, method, body)
	default:
		return nil, fmt.Errorf("unhandled HTTP method '%v' for Shelly method '%v'", verb.HttpMethod, method)
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

func getE(ip net.IP, cmd string, qp types.QueryParams) (*http.Response, error) {

	values := url.Values{}
	for key, value := range qp {
		values.Add(key, value)
	}
	query := values.Encode()

	requestURL := fmt.Sprintf("http://%s/rpc/%s?%s", ip, cmd, query)
	log.Default().Printf("Calling : %v\n", requestURL)

	res, err := http.Get(requestURL)
	if err != nil {
		log.Default().Printf("error making http request: %s\n", err)
		return nil, err
	}
	log.Default().Printf("status code: %d\n", res.StatusCode)

	return res, err
}

func postE(ip net.IP, cmd string, params any) (*http.Response, error) {

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
	log.Default().Printf("Preparing: %v %v body:%v", http.MethodPost, requestURL, string(jsonData))

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

	log.Default().Printf("Calling: %v %v", http.MethodPost, requestURL)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Default().Printf("error making http request: %s", err)
		return nil, err
	}
	log.Default().Printf("status code: %d", res.StatusCode)

	return res, err
}
