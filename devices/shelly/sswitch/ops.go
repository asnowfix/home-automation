package sswitch

import (
	"devices/shelly"
	"encoding/json"
	"log"
)

// func init() {
// 	shelly.RegisterMethod("Switch.GetConfig", GetConfigE, Configuration)
// }

func GetConfigE(d shelly.Device) (*Configuration, error) {

	res, err := shelly.GetE(d, "Switch.GetConfig", shelly.MethodParams{})
	if err != nil {
		return nil, err
	}

	var c Configuration
	err = json.NewDecoder(res.Body).Decode(&c)
	if err != nil {
		return nil, err
	}
	log.Default().Printf("Switch.GetConfig: %v\n", c)

	return &c, nil
}

// func init() {
// 	shelly.RegisterMethod("Switch.GetStatus", GetStatusE)
// }

func GetStatusE(d shelly.Device) (*Status, error) {
	res, err := shelly.GetE(d, "Switch.Status", shelly.MethodParams{})
	if err != nil {
		return nil, err
	}
	var s Status
	err = json.NewDecoder(res.Body).Decode(&s)
	if err != nil {
		return nil, err
	}
	log.Default().Printf("Status: %v\n", s)

	return &s, nil
}

// func ToggleE(d shelly.Device, s sswitch.Toggle) error {
// 	return shelly.GetE(d, "Switch.Toogle")
// }
