package shelly

import (
	"encoding/json"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/hashicorp/mdns"
)

type Product struct {
	Model       string `json:"model"`
	Serial      string `json:"serial"`
	Application string `json:"app"`
	Version     string `json:"ver"`
	Generation  int    `json:"gen"`
}

type Device struct {
	*Product
	Service string                     `json:"service,omitempty"`
	Host    string                     `json:"host"`
	Ipv4    net.IP                     `json:"ipv4"`
	Port    int                        `json:"port"`
	Info    *DeviceInfo                `json:"info"`
	Api     map[string]map[string]bool `json:"api"`
}

type Methods struct {
	Methods []string `json:"methods"`
}

type DeviceInfo struct {
	*Product
	ID                    string `json:"id"`
	MacAddress            string `json:"mac"`
	FirmwareId            string `json:"fw_id"`
	Profile               string `json:"profile"`
	AuthenticationEnabled bool   `json:"auth_en"`
	AuthenticationDomain  string `json:"auth_domain,omitempty"`
	Discoverable          bool   `json:"discoverable"`
	CloudKey              string `json:"key,omitempty"`
	Batch                 string `json:"batch,omitempty"`
	FirmwareSBits         int    `json:"fw_sbits,omitempty"`
}

func NewDevice(entry *mdns.ServiceEntry /**MdnsEntry*/) (*Device, error) {
	log.Default().Printf("Found host:'%v'", entry.Host)
	log.Default().Printf("Found name:'%v'", entry.Name)
	log.Default().Printf("Found ipv4:'%v'", entry.AddrV4)
	log.Default().Printf("Found ipv6:'%v'", entry.AddrV6)
	log.Default().Printf("Found port:'%v'", entry.Port)

	var product = Product{
		Model:  hostRe.ReplaceAllString(entry.Host, "${model}"),
		Serial: hostRe.ReplaceAllString(entry.Host, "${serial}"),
	}

	for i, f := range entry.InfoFields {
		log.Default().Printf("Found info_field[%v]:'%v'", i, f)
		if generationRe.Match([]byte(f)) {
			product.Generation, _ = strconv.Atoi(generationRe.ReplaceAllString(f, "${generation}"))
		}
		if applicationRe.Match([]byte(f)) {
			product.Application = applicationRe.ReplaceAllString(f, "${application}")
		}
		if versionRe.Match([]byte(f)) {
			product.Version = versionRe.ReplaceAllString(f, "${version}")
		}
	}

	// gen, err := strconv.Atoi(genRe.ReplaceAllString(entry.Info, "${gen}"))
	// if err != nil {
	// 	log.Logger.Debug().Msgf("Discarding %v due to %v", pe, err)
	// 	return
	// }

	var device = Device{
		Service: entry.Name,
		Host:    entry.Host,
		Ipv4:    entry.AddrV4,
		Port:    entry.Port,
		Product: &product,
		Api:     make(map[string]map[string]bool),
	}

	return &device, device.init()
}

func (device Device) init() error {
	_, err := device.GetInfo()
	if err != nil {
		return err
	}

	res, err := GetE(device, "Shelly.ListMethods", map[string]string{})
	if err != nil {
		return err
	}
	var ms Methods
	err = json.NewDecoder(res.Body).Decode(&ms)
	if err != nil {
		return err
	}
	log.Default().Printf("Methods: %v\n", ms)
	for _, m := range ms.Methods {
		s := strings.Split(m, ".")[0]
		for api := Shelly; api < None; api++ {
			if s == api.String() {
				if _, exists := device.Api[s]; !exists {
					device.Api[s] = make(map[string]bool)
				}
				device.Api[s][m] = true
			}
		}
	}
	log.Default().Printf("device.Api: %v\n", device.Api)
	return nil
}

func (device Device) GetInfo() (*DeviceInfo, error) {
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
	return device.Info, nil
}
