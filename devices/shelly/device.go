package shelly

import (
	"devices/shelly/types"
	"errors"
	"fmt"
	"log"
	"net"
	"reflect"
	"regexp"
	"strings"
	"sync"
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
	Id         string                                    `json:"id"`
	Service    string                                    `json:"service"`
	MacAddress net.HardwareAddr                          `json:"mac"`
	Host       string                                    `json:"host"`
	Ipv4       net.IP                                    `json:"ipv4"`
	Port       int                                       `json:"port"`
	Info       *DeviceInfo                               `json:"info"`
	Components map[string]map[string]types.MethodHandler `json:"methods"`
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

var nameRe = regexp.MustCompile(fmt.Sprintf("^(?P<id>[a-zA-Z0-9]+).%s.local.$", MDNS_SHELLIES))

var hostRe = regexp.MustCompile("^(?P<model>[a-zA-Z0-9]+)-(?P<serial>[A-Z0-9]+).local.$")

var generationRe = regexp.MustCompile("^gen=(?P<generation>[0-9]+)$")

var applicationRe = regexp.MustCompile("^app=(?P<application>[a-zA-Z0-9]+)$")

var versionRe = regexp.MustCompile("^ver=(?P<version>[.0-9]+)$")

func NewDeviceFromIp(ip net.IP) *Device {
	s := ip.String()
	if d, exists := devicesMap[s]; exists {
		return d
	}
	d := &Device{
		Ipv4: ip,
	}
	d.Init()
	addDevice(s, d)
	return d
}

func (d *Device) Init() *Device {
	m, err := CallE(d, "Shelly", "ListMethods", nil)
	if err != nil {
		log.Default().Printf("Shelly.ListMethods: %v", err)
		return d
	}

	ms := m.(*Methods)
	log.Default().Printf("Shelly.ListMethods: %v", ms)

	d.Components = make(map[string]map[string]types.MethodHandler)
	for _, m := range ms.Methods {
		mi := strings.Split(m, ".")
		c := mi[0] // component
		v := mi[1] // verb
		for component := types.Shelly; component < types.None; component++ {
			if c == component.String() {
				if _, exists := d.Components[c]; !exists {
					d.Components[c] = make(map[string]types.MethodHandler)
				}
				if _, exists := methods[c]; exists {
					if _, exists := methods[c][v]; exists {
						d.Components[c][v] = methods[c][v]
					}
				}
			}
		}
	}
	log.Default().Printf("device.Api: %v", d.Components)

	di, err := CallE(d, "Shelly", "GetDeviceInfo", nil)
	if err != nil {
		log.Default().Printf("Shelly.GetDeviceInfo: %v", err)
		return d
	}
	d.Info = di.(*DeviceInfo)
	log.Default().Printf("Shelly.GetDeviceInfo: loaded %v", *d.Info)

	d.Id = d.Info.Id

	return d
}

func (d *Device) Topics() []string {
	topics := make([]string, 5)
	topics[0] = fmt.Sprintf("%s/rpc", d.Info.Id)
	topics[0] = fmt.Sprintf("%s/events/rpc", d.Info.Id)
	topics[0] = fmt.Sprintf("%s/online", d.Info.Id)
	return topics
}

var devicesMap map[string]*Device = make(map[string]*Device)

var devicesMutex sync.Mutex

func Devices() map[string]*Device {
	devices, _ := DevicesE()
	return devices
}

func DevicesE() (map[string]*Device, error) {
	devicesMutex.Lock()
	if len(devicesMap) == 0 {
		err := loadDevicesFromMdns()
		if err != nil {
			log.Default().Fatal(err)
			return nil, err
		}
		log.Default().Printf("Discovered %v devices", len(devicesMap))
	}
	log.Default().Printf("Knows %v devices (%v)", len(devicesMap), devicesMap)
	devicesMutex.Unlock()
	return devicesMap, nil
}

func addDevice(name string, device *Device) {
	if _, exists := devicesMap[name]; !exists {
		log.Default().Printf("Loading %v (%v)", name, *device)
		devicesMap[name] = device
	} else {
		log.Default().Printf("Already loaded %v", name)
	}
}

func Lookup(name string) (*Device, error) {
	ip := net.ParseIP(name)
	if ip != nil {
		log.Default().Printf("Contacting device IP '%v'", ip)
		return NewDeviceFromIp(ip), nil
	} else {
		devices := Devices()
		log.Default().Printf("Looking-up '%v' in devices %v", name, devices)

		for key, device := range devices {
			log.Default().Printf("Matching '%v' against %v", name, device)

			if key == name {
				return device, nil
			}
			if device.Info != nil && device.Info.MacAddress != nil && device.Info.MacAddress.String() == name {
				return device, nil
			}
			if device.Ipv4 != nil && device.Ipv4.String() == name {
				return device, nil
			}
			if device.Host == name {
				return device, nil
			}
			if device.Model == name {
				return device, nil
			}
		}
		return nil, errors.New("No device matching '" + name + "'")
	}
}

type Do func(*Device) (*Device, error)

func Foreach(args []string, do Do) error {
	log.Default().Printf("Running %v on %v", reflect.TypeOf(do), args)

	if len(args) > 0 {
		for _, name := range args {
			log.Default().Printf("Looking for Shelly device %v", name)
			device, err := Lookup(name)
			if err != nil {
				log.Default().Printf("Skipping device %v: %v", name, err)
				continue
			}
			_, err = do(device)
			if err != nil {
				log.Default().Printf("Operation on %v failed: %v", name, err)
				continue
			}
		}
	} else {
		log.Default().Print("Running on every device")
		for _, device := range Devices() {
			do(device)
		}
	}
	return nil
}
