package myip

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

func SeeIp() string {
	res, _ := http.Get("https://ipv4.seeip.org/jsonip")
	ipJson, _ := ioutil.ReadAll(res.Body)
	var data struct {
		Ip string `json:"ip"`
	}
	if err := json.Unmarshal([]byte(ipJson), &data); err != nil {
		panic(err)
	}
	fmt.Printf("My IPv4 is" + data.Ip)
	return data.Ip
}
