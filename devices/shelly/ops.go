package shelly

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
)

type MethodConfiguration struct {
	Allocate func() any
	Params   map[string]string
}

var methods map[string]MethodConfiguration

func ConfigureMethod(m string, c MethodConfiguration) {
	log.Default().Printf("Configuring method:%v: params:%v\n", m, c.Params)
	if _, exists := methods[m]; !exists {
		methods[m] = c
	}
}

func CallMethod(device *Device, m string) (any, error) {
	var data any
	var params map[string]string
	if method, exists := methods[m]; exists {
		data = method.Allocate()
		params = method.Params
		log.Default().Printf("Found configuration for method: %v: parser:%v params:%v\n", m, reflect.TypeOf(data), params)
	} else {
		log.Default().Printf("Did not find any configuration for method: %v\n", m)
		params = make(map[string]string)
	}

	res, err := GetE(device, m, params)
	if err != nil {
		return nil, err
	}
	err = json.NewDecoder(res.Body).Decode(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetE(d *Device, cmd string, params MethodParams) (*http.Response, error) {

	values := url.Values{}
	for key, value := range params {
		values.Add(key, value)

	}
	query := values.Encode()

	requestURL := fmt.Sprintf("http://%s/rpc/%s?%s", d.Host, cmd, query)
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
