package shelly

import (
	"devices/shelly/input"
	"devices/shelly/mqtt"
	"devices/shelly/script"
	"devices/shelly/sswitch"
	"devices/shelly/system"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
)

var methods map[string]map[string]types.MethodConfiguration

func init() {
	methods = make(map[string]map[string]types.MethodConfiguration)

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
	ConfigureMethod("Shelly", "GetDeviceInfo", types.MethodConfiguration{
		Allocate: func() any { return new(DeviceInfo) },
		Params: map[string]string{
			"ident": "true",
		},
	})

	system.Init(ConfigureMethod)
	sswitch.Init(ConfigureMethod)
	mqtt.Init(ConfigureMethod)
	script.Init(ConfigureMethod)
	input.Init(ConfigureMethod)

	log.Default().Printf("configured %v APIs", len(methods))
}

func ConfigureMethod(a string, v string, c types.MethodConfiguration) {
	log.Default().Printf("Configuring method:%v.%v...", a, v)
	if _, exists := methods[a]; !exists {
		methods[a] = make(map[string]types.MethodConfiguration)
		log.Default().Printf("... Added API:%v", a)
	}
	if _, exists := methods[a][v]; !exists {
		methods[a][v] = c
		log.Default().Printf("... Added verb:%v.%v: params:%v", a, v, c.Params)
	}
}

func CallMethod(device *Device, a string, v string) any {
	data, err := CallMethodE(device, a, v)
	if err != nil {
		log.Default().Print(err)
		panic(err)
	}
	return data
}

func CallMethodE(device *Device, a string, v string) (any, error) {
	var data any = nil
	var params map[string]string

	if api, exists := device.Api[a]; exists {
		if verb, exists := api[v]; exists {
			log.Default().Printf("found configuration for method: %v.%v: parser:%v params:%v", a, v, reflect.TypeOf(data), params)
			data = verb.Allocate()
			params = verb.Params
		}
	}

	if data == nil {
		return nil, fmt.Errorf("did not find any configuration for method: %v.%v", a, v)
	}

	res, err := GetE(device, fmt.Sprintf("%v.%v", a, v), params)
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

	// req, err := http.NewRequest("GET", "http://api.themoviedb.org/3/tv/popular", nil)
	// if err != nil {
	// 	log.Print(err)
	// 	os.Exit(1)
	// }
	// q := req.URL.Query()
	// q.Add("api_key", "key_from_environment_or_flag")
	// q.Add("another_thing", "foo & bar")
	// req.URL.RawQuery = q.Encode()

	// // req, _ := http.NewRequest("GET", "http://api.themoviedb.org/3/tv/popular", nil)
	// // req.Header.Add("Accept", "application/json")
	// resp, err := client.Do(req)

	// defer res.Body.Close()
	// b, err := io.ReadAll(res.Body)
	// if err != nil {
	// 	log.Fatalln(err)
	// }
	// log.Default().Printf("res: %s\n", string(b))

	return res, err
}
