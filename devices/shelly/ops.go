package shelly

import (
	"fmt"
	"log"
	"net/http"
)

// type Method interface {
// 	ReturnedType
// 	func(Device) (*any, error)
// }

// var methods *map[string]Method

// func RegisterMethod(name string, method Method, data interface) {
// 	if methods == nil {
// 		methods = new(map[string]Method)
// 	}
// 	(*methods)[name] = method
// }

func GetE(d Device, cmd string, params MethodParams) (*http.Response, error) {
	requestURL := fmt.Sprintf("http://%s/rpc/%s?id=0", d.Host, cmd)

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
