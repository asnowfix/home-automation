package shelly

import (
	"bytes"
	"devices/shelly/input"
	"devices/shelly/mqtt"
	"devices/shelly/script"
	"devices/shelly/sswitch"
	"devices/shelly/system"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
)

var methods map[string]map[string]types.MethodHandler

func Init() {
	methods = make(map[string]map[string]types.MethodHandler)

	// Shelly.ListMethods
	// Shelly.PutTLSClientKey
	// Shelly.PutTLSClientCert
	// Shelly.PutUserCA
	// Shelly.Reboot
	// Shelly.SetAuth
	// Shelly.Update
	// Shelly.CheckForUpdate
	// Shelly.DetectLocation
	// Shelly.ListTimezones
	// Shelly.GetComponents
	// Shelly.GetStatus
	// Shelly.FactoryReset
	// Shelly.ResetWiFiConfig
	// Shelly.GetConfig
	// Shelly.GetDeviceInfo
	RegisterMethodHandler("Shelly", "GetDeviceInfo", types.MethodHandler{
		Allocate: func() any { return new(DeviceInfo) },
		Params: map[string]string{
			"ident": "true",
		},
		HttpMethod: http.MethodGet,
	})

	system.Init(RegisterMethodHandler)
	sswitch.Init(RegisterMethodHandler)
	mqtt.Init(RegisterMethodHandler)
	script.Init(RegisterMethodHandler)
	input.Init(RegisterMethodHandler)

	log.Default().Printf("Registered %v APIs", len(methods))
}

func RegisterMethodHandler(c string, v string, m types.MethodHandler) {
	log.Default().Printf("Registering handler for method:%v.%v...", c, v)
	if _, exists := methods[c]; !exists {
		methods[c] = make(map[string]types.MethodHandler)
		log.Default().Printf("... Added API:%v", c)
	}
	if _, exists := methods[c][v]; !exists {
		methods[c][v] = m
		log.Default().Printf("... Added verb:%v.%v: http(method=%v params=%v)", c, v, m.HttpMethod, m.Params)
	}
}

func CallMethod(device *Device, component string, verb string, params any) any {
	data, err := CallMethodE(device, component, verb, params)
	if err != nil {
		log.Default().Print(err)
		panic(err)
	}
	return data
}

func CallMethodE(device *Device, c string, v string, params any) (any, error) {
	var data any = nil

	var verb types.MethodHandler

	if comp, exists := device.Components[c]; exists {
		if verb, exists = comp[v]; exists {
			data = verb.Allocate()
			log.Default().Printf("found configuration for method: %v.%v: parser:%v params:%v", c, v, reflect.TypeOf(data), params)
		}
	}

	if data == nil {
		return nil, fmt.Errorf("did not find any configuration for method: %v.%v", c, v)
	}

	var res *http.Response
	var err error

	switch verb.HttpMethod {
	case http.MethodGet:
		res, err = GetE(device, fmt.Sprintf("%v.%v", c, v), verb.Params)
	case http.MethodPost:
		res, err = PostE(device, fmt.Sprintf("%v.%v", c, v), params)
	default:
		return nil, fmt.Errorf("unhandled HTTP method '%v' for Shelly verb '%v.%v'", verb.HttpMethod, c, v)
	}

	if err != nil {
		return nil, err
	}

	err = json.NewDecoder(res.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func GetE(d *Device, cmd string, params types.MethodParams) (*http.Response, error) {

	values := url.Values{}
	for key, value := range params {
		values.Add(key, value)

	}
	query := values.Encode()

	requestURL := fmt.Sprintf("http://%s/rpc/%s?%s", d.Ipv4, cmd, query)
	log.Default().Printf("Calling : %v\n", requestURL)

	res, err := http.Get(requestURL)
	if err != nil {
		log.Default().Printf("error making http request: %s\n", err)
		return nil, err
	}
	log.Default().Printf("status code: %d\n", res.StatusCode)

	return res, err
}

func PostE(d *Device, cmd string, params any) (*http.Response, error) {

	var payload struct {
		Method string `json:"method"`
		Params any    `json:"params"`
	}

	payload.Method = cmd
	payload.Params = params

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	requestURL := fmt.Sprintf("http://%s/rpc", d.Ipv4)
	log.Default().Printf("Calling : %v\n", requestURL)

	json.Marshal(params)
	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}

	// q := req.URL.Query()
	// q.Add("api_key", "key_from_environment_or_flag")
	// q.Add("another_thing", "foo & bar")
	// req.URL.RawQuery = q.Encode()

	// req.Header.Add("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Default().Printf("error making http request: %s\n", err)
		return nil, err
	}
	log.Default().Printf("status code: %d\n", res.StatusCode)

	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		log.Fatalln(err)
	}
	log.Default().Printf("res: %s\n", string(b))

	return res, err
}
