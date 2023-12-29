package tapo

import (
	"encoding/json"
	"log"
	"net"
	"os"

	"github.com/j-iot/tapo-go"
)

var username string = os.Getenv("TAPO_USERNAME")

var password string = os.Getenv("TAPO_PASSWORD")

type Switch struct {
	device *tapo.Device
}

func NewSwitch(ip net.IP) (*Switch, error) {
	var s Switch

	s.device = tapo.New(ip.String(), username, password)
	if err := s.device.Handshake(); err != nil {
		log.Default().Print(err)
		return nil, err
	}

	if err := s.device.Login(); err != nil {
		log.Default().Print(err)
		return nil, err
	}

	return &s, nil
}

func (s Switch) Set() error {
	return s.device.Switch(true)
}

func (s Switch) Unset() error {
	return s.device.Switch(false)
}

func (s Switch) GetStatus() (bool, error) {
	deviceInfo, err := s.device.GetDeviceInfo()
	if err != nil {
		log.Default().Print(err)
		return false, err
	}
	return deviceInfo.Result.DeviceON, nil
}

func (s Switch) GetInfo() (any, error) {
	deviceInfo, err := s.device.GetDeviceInfo()
	if err != nil {
		log.Default().Print(err)
		return nil, err
	}
	log.Default().Print(deviceInfo.Result)
	return json.Marshal(deviceInfo.Result)
}
