package shelly

import (
	"devices"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"
)

type Product struct {
	Model       string           `json:"model"`
	Serial      string           `json:"serial,omitempty"`
	MacAddress  net.HardwareAddr `json:"mac"`
	Application string           `json:"app"`
	Version     string           `json:"ver"`
	Generation  int              `json:"gen"`
}

type Device struct {
	Product
	Service    string                                          `json:"service"`
	MacAddress net.HardwareAddr                                `json:"mac"`
	Host       string                                          `json:"host"`
	Ipv4       net.IP                                          `json:"ipv4"`
	Port       int                                             `json:"port"`
	Info       *DeviceInfo                                     `json:"info"`
	Api        map[string]map[string]types.MethodConfiguration `json:"api"`
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

func NewDevice(d string) (*Device, error) {
	var device Device
	if ip := net.ParseIP(d); ip != nil {
		device = Device{
			Ipv4: ip,
		}
		return getDeviceInfo(&device)
	} else {
		hosts, err := devices.List()
		if err != nil {
			return nil, err
		}
		for _, host := range hosts {
			if d == host.Mac.String() {
				device = Device{
					Ipv4:       host.Ip,
					MacAddress: host.Mac,
				}
				return getDeviceInfo(&device)
			}
		}
	}
	return nil, fmt.Errorf("device not found: %v", d)
}

func getDeviceInfo(device *Device) (*Device, error) {
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
	device.Api = make(map[string]map[string]types.MethodConfiguration)
	for _, m := range ms.Methods {
		mi := strings.Split(m, ".")
		a := mi[0] // api
		v := mi[1] // verb
		for api := types.Shelly; api < types.None; api++ {
			if a == api.String() {
				if _, exists := device.Api[a]; !exists {
					device.Api[a] = make(map[string]types.MethodConfiguration)
				}
				if _, exists := methods[a]; exists {
					if _, exists := methods[a][v]; exists {
						device.Api[a][v] = methods[a][v]
					}
				}
			}
		}
	}
	log.Default().Printf("device.Api: %v\n", device.Api)

	if device.Info == nil {
		device.Info = CallMethod(device, "Shelly", "GetDeviceInfo").(*DeviceInfo)
		log.Default().Printf("Shelly.GetDeviceInfo: loaded %v\n", *device.Info)
	}

	return device, nil
}
