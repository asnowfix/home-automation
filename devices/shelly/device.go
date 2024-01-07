package shelly

import (
	"devices/shelly/types"
	"encoding/json"
	"log"
	"net"
	"regexp"
	"strings"
)

type Product struct {
	Model       string `json:"model"`
	Serial      string `json:"serial,omitempty"`
	MacAddress  string `json:"mac"`
	Application string `json:"app"`
	Version     string `json:"ver"`
	Generation  int    `json:"gen"`
}

type Device struct {
	Product
	Service string                    `json:"service"`
	Host    string                    `json:"host"`
	Ipv4    net.IP                    `json:"ipv4"`
	Port    int                       `json:"port"`
	Info    *DeviceInfo               `json:"info"`
	Api     map[string]map[string]any `json:"api"`
}

type Methods struct {
	Methods []string `json:"methods"`
}

type DeviceInfo struct {
	*Product
	Id                    string `json:"id"`
	FirmwareId            string `json:"fw_id"`
	Profile               string `json:"profile,omitempty"`
	AuthenticationEnabled bool   `json:"auth_en"`
	AuthenticationDomain  string `json:"auth_domain,omitempty"`
	Discoverable          bool   `json:"discoverable"`
	CloudKey              string `json:"key,omitempty"`
	Batch                 string `json:"batch,omitempty"`
	FirmwareSBits         string `json:"fw_sbits,omitempty"`
}

var hostRe = regexp.MustCompile("^(?P<model>[a-zA-Z0-9]+)-(?P<mac>[A-Z0-9]+).local.$")

var generationRe = regexp.MustCompile("^gen=(?P<generation>[0-9]+)$")

var applicationRe = regexp.MustCompile("^app=(?P<application>[a-zA-Z0-9]+)$")

var versionRe = regexp.MustCompile("^ver=(?P<version>[.0-9]+)$")

func NewDevice(ip net.IP) (*Device, error) {

	var device Device = Device{
		Ipv4: ip,
	}
	return getDeviceInfo(&device)
}

func getDeviceInfo(device *Device) (*Device, error) {
	device.Api = make(map[string]map[string]any)
	if device.Info == nil {
		res, err := GetE(device, "Shelly.GetDeviceInfo", map[string]string{
			"ident": "true",
		})
		if err != nil {
			return nil, err
		}
		var di DeviceInfo
		err = json.NewDecoder(res.Body).Decode(&di)
		if err != nil {
			return nil, err
		}
		log.Default().Printf("Shelly.GetDeviceInfo: loaded %v\n", di)
		device.Info = &di
	}

	res, err := GetE(device, "Shelly.ListMethods", map[string]string{})
	if err != nil {
		return nil, err
	}

	var ms Methods
	err = json.NewDecoder(res.Body).Decode(&ms)
	if err != nil {
		return nil, err
	}

	log.Default().Printf("Shelly.ListMethods: %v\n", ms)
	for _, m := range ms.Methods {
		mi := strings.Split(m, ".")
		a := mi[0] // api
		v := mi[1] // verb
		for api := types.Shelly; api < types.None; api++ {
			if a == api.String() {
				if _, exists := device.Api[a]; !exists {
					device.Api[a] = make(map[string]any)
				}
				device.Api[a][v] = nil
			}
		}
	}
	log.Default().Printf("device.Api: %v\n", device.Api)

	for apiName, api := range device.Api {
		if _, exists := api["GetConfig"]; exists {
			data, err := CallMethod(device, apiName+".GetConfig")
			if err != nil {
				return nil, err
			}
			api["GetConfig"] = data
		}
	}

	for apiName, api := range device.Api {
		if _, exists := api["GetStatus"]; exists {
			data, err := CallMethod(device, apiName+".GetStatus")
			if err != nil {
				return nil, err
			}
			api["GetStatus"] = data
		}
	}

	return device, nil
}
