package shelly

import (
	"devices/shelly/types"
	"encoding/json"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/mdns"
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

func NewDevice(entry *mdns.ServiceEntry /**MdnsEntry*/) (*Device, error) {
	log.Default().Printf("Found host:'%v'", entry.Host)
	log.Default().Printf("Found name:'%v'", entry.Name)
	log.Default().Printf("Found ipv4:'%v'", entry.AddrV4)
	log.Default().Printf("Found ipv6:'%v'", entry.AddrV6)
	log.Default().Printf("Found port:'%v'", entry.Port)

	var generation int
	var application string
	var version string
	for i, f := range entry.InfoFields {
		log.Default().Printf("Found info_field[%v]:'%v'", i, f)
		if generationRe.Match([]byte(f)) {
			generation, _ = strconv.Atoi(generationRe.ReplaceAllString(f, "${generation}"))
		}
		if applicationRe.Match([]byte(f)) {
			application = applicationRe.ReplaceAllString(f, "${application}")
		}
		if versionRe.Match([]byte(f)) {
			version = versionRe.ReplaceAllString(f, "${version}")
		}
	}

	var device = Device{
		Service: entry.Name,
		Host:    entry.Host,
		Ipv4:    entry.AddrV4,
		Port:    entry.Port,
		Product: Product{
			Model:       hostRe.ReplaceAllString(entry.Host, "${model}"),
			MacAddress:  hostRe.ReplaceAllString(entry.Host, "${mac}"),
			Generation:  generation,
			Application: application,
			Version:     version,
		},
		Api: make(map[string]map[string]any),
	}

	// gen, err := strconv.Atoi(genRe.ReplaceAllString(entry.Info, "${gen}"))
	// if err != nil {
	// 	log.Logger.Debug().Msgf("Discarding %v due to %v", pe, err)
	// 	return
	// }

	if device.Info == nil {
		res, err := GetE(&device, "Shelly.GetDeviceInfo", map[string]string{
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

	res, err := GetE(&device, "Shelly.ListMethods", map[string]string{})
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
			// log.Default().Printf("%v.GetConfig return %v\n", apiName, methodOutput[apiName+".GetConfig"])

			data, err := CallMethod(&device, apiName+".GetConfig")
			if err != nil {
				return nil, err
			}

			// log.Default().Printf("%v.GetConfig return %v\n", apiName, methodOutput[apiName+".GetConfig"])

			// res, err := GetE(device, apiName+".GetConfig", MethodParams{})
			// if err != nil {
			// 	return nil, err
			// }
			// err = json.NewDecoder(res.Body).Decode(&data)
			// if err != nil {
			// 	return nil, err
			// }
			// log.Default().Printf("%v.GetConfig: got %v\n", apiName, data)
			api["GetConfig"] = data
		}
	}

	return &device, nil
}
