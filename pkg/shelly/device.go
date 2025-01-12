package shelly

import (
	"encoding/json"
	"fmt"
	"mymqtt"
	"net"
	"os"
	"pkg/shelly/types"
	"reflect"
	"regexp"
	"strings"

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
	Product
	Id_        string                                    `json:"id"`
	Service    string                                    `json:"service"`
	Host       string                                    `json:"host"`
	Ipv4_      net.IP                                    `json:"ipv4"`
	Port       int                                       `json:"port"`
	Info       *DeviceInfo                               `json:"info"`
	Methods    []string                                  `json:"methods"`
	Components map[string]map[string]types.MethodHandler `json:"-"`
	me         string                                    `json:"-"`
	to         chan []byte                               `json:"-"`
	from       chan []byte                               `json:"-"`
}

func (d *Device) Id() string {
	return d.Id_
}

func (d *Device) Ipv4() net.IP {
	return d.Ipv4_
}

type MethodsResponse struct {
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

func (d *Device) String() string {
	return fmt.Sprintf("%s_%s", d.Model, d.Id_)
}

func (d *Device) To() chan<- []byte {
	return d.to
}

func (d *Device) From() <-chan []byte {
	return d.from
}

func (d *Device) ReplyTo() string {
	return d.me
}

func NewDeviceFromIp(log logr.Logger, mc *mymqtt.Client, ip net.IP) *Device {
	d := &Device{
		Ipv4_: ip,
		Host:  ip.String(),
	}
	d.Init(log, mc, types.ChannelHttp)
	return d
}

func NewDeviceFromId(log logr.Logger, mc *mymqtt.Client, id string) *Device {
	d := &Device{
		Id_:  id,
		Host: fmt.Sprintf("%s.local", id),
	}
	d.Init(log, mc, types.ChannelMqtt)
	return d
}

func (d *Device) Init(log logr.Logger, mc *mymqtt.Client, via types.Channel) error {
	var err error

	hostname, err := os.Hostname()
	if err != nil {
		log.Error(err, "Unable to get local hostname")
		return err
	}
	d.me = fmt.Sprintf("%s_%s", hostname, d.Id_)
	d.from, err = mc.Subscribe(fmt.Sprintf("%s/rpc", d.me), 1 /*qlen*/)
	if err != nil {
		log.Error(err, "Unable to subscribe to device's RPC topic", "device_id", d.Id_)
		return err
	}

	d.to = make(chan []byte, 1 /*qlen*/)
	toDevice := fmt.Sprintf("%s/rpc", d.Id_)
	go func() {
		for {
			msg := <-d.to
			mc.Publish(toDevice, msg)
		}
	}()

	m, err := GetRegistrar().CallE(d, via, listMethodsHandler, nil)
	if err != nil {
		return err
	}

	d.Methods = m.(*MethodsResponse).Methods
	log.Info("Shelly.ListMethods", "methods", d.Methods)

	d.Components = make(map[string]map[string]types.MethodHandler)
	for _, m := range d.Methods {
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
	log.Info("device API", "components", d.Components)

	di, err := d.CallE(via, "Shelly", "GetDeviceInfo", map[string]interface{}{"ident": true})
	if err != nil {
		return err
	}
	d.Info = di.(*DeviceInfo)
	log.Info("Shelly.GetDeviceInfo: loaded", "info", *d.Info)
	d.Id_ = d.Info.Id
	d.MacAddress = d.Info.MacAddress

	return nil
}

type Do func(logr.Logger, types.Channel, *Device, []string) (any, error)

func Print(log logr.Logger, d any) error {
	buf, err := json.Marshal(d)
	if err != nil {
		log.Error(err, "Unable to JSON-ify", "out", d)
		return err
	}
	fmt.Print(string(buf))
	return nil
}

func Foreach(log logr.Logger, mc *mymqtt.Client, names []string, via types.Channel, do Do, args []string) error {
	log.Info("Running", "func", reflect.TypeOf(do), "args", args)

	if len(names) > 0 {
		for _, name := range names {
			log.Info("Looking for Shelly device", "name", name)
			var device *Device
			var ip net.IP
			if ip = net.ParseIP(name); ip != nil {
				device = NewDeviceFromIp(log, mc, ip)
			} else {
				device = NewDeviceFromId(log, mc, name)
			}
			out, err := do(log, via, device, args)
			if err != nil {
				log.Error(err, "Operation failed", "device", name)
				continue
			}
			s, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Print(string(s))
		}
	} else {
		log.Info("Running on every device")
		out := make([]any, 0)
		// FIXME: implement lookup with request to myhome
		devices := make(map[string]*Device, 0)
		for _, device := range devices {
			item, err := do(log, via, device, args)
			if err != nil {
				log.Error(err, "Operation failed", "device", device.Host)
				continue
			}
			out = append(out, item)
		}
		s, err := json.Marshal(out)
		if err != nil {
			return err
		}
		fmt.Print(string(s))
	}
	return nil
}
