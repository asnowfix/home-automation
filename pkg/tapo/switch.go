package tapo

import (
	"encoding/json"
	"net"
	"os"

	"github.com/go-logr/logr"
	"github.com/j-iot/tapo-go"
)

var username string = os.Getenv("TAPO_USERNAME")

var password string = os.Getenv("TAPO_PASSWORD")

type Switch struct {
	log    logr.Logger
	device *tapo.Device
}

func NewSwitch(log logr.Logger, ip net.IP) (*Switch, error) {
	var s Switch = Switch{
		log: log,
	}

	s.device = tapo.New(ip.String(), username, password)
	log.Info("Tapo device: %v", s.device)

	log.Info("Tapo Handshake...")
	if err := s.device.Handshake(); err != nil {
		log.Error(err, "Tapo Handshake failed")
		return nil, err
	}
	log.Info("Tapo Handshake... Ok")

	log.Info("Tapo Login...")
	if err := s.device.Login(); err != nil {
		log.Error(err, "Tapo Login failed")
		return nil, err
	}
	log.Info("Tapo Login... Ok")

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
		s.log.Error(err, "Failed to get device info")
		return false, err
	}
	return deviceInfo.Result.DeviceON, nil
}

func (s Switch) GetInfo() (any, error) {
	deviceInfo, err := s.device.GetDeviceInfo()
	if err != nil {
		s.log.Error(err, "Failed to get device info")
		return nil, err
	}
	s.log.Info("device info", deviceInfo.Result)
	return json.Marshal(deviceInfo.Result)
}
