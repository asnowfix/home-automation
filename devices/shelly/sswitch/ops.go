package sswitch

import (
	"devices/shelly"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func GetConfigE(d shelly.Device) (*Configuration, error) {
	requestURL := fmt.Sprintf("http://%s/rpc/%s?id=0", d.Host, "Switch.GetConfig")

	res, err := http.Get(requestURL)
	if err != nil {
		log.Default().Printf("error making http requestu: %s\n", err)
		return nil, err
	}
	log.Default().Printf("status code: %d\n", res.StatusCode)

	// defer res.Body.Close()
	// b, err := io.ReadAll(res.Body)
	// if err != nil {
	// 	log.Fatalln(err)
	// }
	// log.Default().Printf("res: %s\n", string(b))

	var c Configuration
	err = json.NewDecoder(res.Body).Decode(&c)
	if err != nil {
		return nil, err
	}
	log.Default().Printf("GetConfig: %v\n", c)

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

	return &c, nil
}

func GetConfig(d shelly.Device) *Configuration {
	c, err := GetConfigE(d)
	if err != nil {
		panic(err)
	}
	return c
}
