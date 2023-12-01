package sswitch

import (
	"devices/shelly"
	"devices/shelly/sswitch"
	"encoding/json"
	"log"
)

func SwitchGetConfigE(d shelly.Device) (*Configuration, error) {

	res, err := shelly.GetE(d, "Switch.GetConfig")
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

func SwitchGetConfig(d shelly.Device) *Configuration {
	c, err := SwitchGetConfigE(d)
	if err != nil {
		panic(err)
	}
	return c
}

func SwitchStatusE(d shelly.Device) (*Status, error) {
	res, err := shelly.GetE(d, "Switch.Status")
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

func SwitchStatus(d shelly.Device) *Status {
	s, err := SwitchStatusE(d)
	if err != nil {
		panic(err)
	}
	return s
}

func SwitchToggleE(d shelly.Device, s sswitch.Toggle) error {
	return shelly.GetE(d, "Switch.Toogle")
}

func SwitchSetE(d shelly.Device, s sswitch.Set) error {
	return shelly.GetE(d, "Switch.Set")
}
