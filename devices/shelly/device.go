package shelly

import (
	"devices/shelly/types"
	"errors"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/go-logr/logr"
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
	log logr.Logger
	Product
	Id_        string                                    `json:"id"`
	Service    string                                    `json:"service"`
	MacAddress net.HardwareAddr                          `json:"mac"`
	Host       string                                    `json:"host"`
	Ipv4_      net.IP                                    `json:"ipv4"`
	Port       int                                       `json:"port"`
	Info       *DeviceInfo                               `json:"info"`
	Components map[string]map[string]types.MethodHandler `json:"methods"`
}

func (d *Device) Id() string {
	return d.Id_
}

func (d *Device) Ipv4() net.IP {
	return d.Ipv4_
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

func (d *Device) Call(ch types.Channel, component string, verb string, params any, errv any) any {
	data, err := d.CallE(ch, component, verb, params)
	if err != nil {
		d.log.Info("failure calling device %v: %v", d.Id(), err)
		return errv
	}
	return data
}

func (d *Device) MethodHandlerE(c string, v string) (types.MethodHandler, error) {
	var mh types.MethodHandler

	if c == "Shelly" && v == "ListMethods" {
		mh = listMethodsHandler
	} else {
		found := false
		if comp, exists := d.Components[c]; exists {
			if mh, exists = comp[v]; exists {
				found = true
			}
		}
		if !found {
			return types.MethodNotFound, fmt.Errorf("did not find any handler for method: %v.%v", c, v)
		}
	}
	return mh, nil
}

func (d *Device) CallE(ch types.Channel, comp string, verb string, params any) (any, error) {
	mh, err := d.MethodHandlerE(comp, verb)
	if err != nil {
		return nil, err
	}
	return GetRegistrar().CallE(d, ch, mh, params)
}

func NewDeviceFromIp(log logr.Logger, ip net.IP) *Device {
	s := ip.String()
	if d, exists := devicesMap[s]; exists {
		return d
	}
	d := &Device{
		log:   log,
		Ipv4_: ip,
	}
	d.Init(types.ChannelHttp)
	addDevice(log, s, d)
	return d
}

func (d *Device) Init(ch types.Channel) *Device {
	m, err := GetRegistrar().CallE(d, ch, listMethodsHandler, nil)
	if err != nil {
		d.log.Error(err, "Shelly.ListMethods")
		return d
	}

	ms := m.(*Methods)
	d.log.Info("Shelly.ListMethods", "methods", ms)

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
				if _, exists := registrar.methods[c]; exists {
					if _, exists := registrar.methods[c][v]; exists {
						d.Components[c][v] = registrar.methods[c][v]
					}
				}
			}
		}
	}
	d.log.Info("device API", "components", d.Components)

	di, err := d.CallE(ch, "Shelly", "GetDeviceInfo", nil)
	if err != nil {
		d.log.Info("Shelly.GetDeviceInfo: %v", err)
		return d
	}
	d.Info = di.(*DeviceInfo)
	d.log.Info("Shelly.GetDeviceInfo: loaded %v", *d.Info)

	d.Id_ = d.Info.Id

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

func Devices(log logr.Logger) map[string]*Device {
	devices, _ := DevicesE(log)
	return devices
}

func DevicesE(log logr.Logger) (map[string]*Device, error) {
	devicesMutex.Lock()
	if len(devicesMap) == 0 {
		err := loadDevicesFromMdns(log)
		if err != nil {
			log.Error(err, "Unable to load devices from mDNS")
			return nil, err
		}
		log.Info("Discovered %v devices", len(devicesMap))
	}
	log.Info("Knows %v devices (%v)", len(devicesMap), devicesMap)
	devicesMutex.Unlock()
	return devicesMap, nil
}

func addDevice(log logr.Logger, name string, device *Device) {
	if _, exists := devicesMap[name]; !exists {
		log.Info("Loading", "name", name, "device", *device)
		devicesMap[name] = device
	} else {
		log.Info("Already loaded", "name", name)
	}
}

func Lookup(log logr.Logger, name string) (*Device, error) {
	ip := net.ParseIP(name)
	if ip != nil {
		log.Info("Contacting device", "ip", ip)
		return NewDeviceFromIp(log, ip), nil
	} else {
		devices := Devices(log)
		log.Info("Looking-up in...", "name", name, "devices", devices)

		for key, device := range devices {
			log.Info("Matching", "name", name, "device", device)

			if key == name {
				return device, nil
			}
			if device.Info != nil && device.Info.MacAddress != nil && device.Info.MacAddress.String() == name {
				return device, nil
			}
			if device.Ipv4() != nil && device.Ipv4().String() == name {
				return device, nil
			}
			if device.Host == name {
				return device, nil
			}
			if device.Model == name {
				return device, nil
			}
		}
		return nil, errors.New("No device matching name:'" + name + "'")
	}
}

type Do func(logr.Logger, types.Channel, *Device) (*Device, error)

func Foreach(log logr.Logger, args []string, via types.Channel, do Do) error {
	log.Info("Running", "func", reflect.TypeOf(do), "args", args)

	if len(args) > 0 {
		for _, name := range args {
			log.Info("Looking for Shelly device", "name", name)
			device, err := Lookup(log, name)
			if err != nil {
				log.Info("Skipping device %v: %v", name, err)
				continue
			}
			_, err = do(log, via, device)
			if err != nil {
				log.Info("Operation on %v failed: %v", name, err)
				continue
			}
		}
	} else {
		log.Info("Running on every device")
		for _, device := range Devices(log) {
			do(log, via, device)
		}
	}
	return nil
}
