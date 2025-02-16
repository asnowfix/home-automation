package shelly

import (
	"context"
	"devices/shelly/wifi"
	"encoding/json"
	"fmt"
	"mymqtt"
	"net"
	"os"
	"os/signal"
	"pkg/shelly/mqtt"
	"pkg/shelly/sswitch"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"reflect"
	"regexp"
	"schedule"
	"strings"

	"github.com/go-logr/logr"
)

const Shelly = "Shelly"

type Product struct {
	Model       string           `json:"model"`
	Serial      string           `json:"serial,omitempty"`
	MacAddress  net.HardwareAddr `json:"mac"`
	Application string           `json:"app"`
	Version     string           `json:"ver"`
	Generation  int              `json:"gen"`
}

type State uint

const (
	New State = iota
	HttpOk
	MqttOk
)

type Device struct {
	Product
	Id_               string                                    `json:"id"`
	Service           string                                    `json:"service"`
	Host              string                                    `json:"host"`
	Ipv4_             net.IP                                    `json:"ipv4"`
	Port              int                                       `json:"port"`
	Info              *DeviceInfo                               `json:"info"`
	Methods           []string                                  `json:"methods"`
	ComponentsMethods map[string]map[string]types.MethodHandler `json:"-"`
	Components        *[]Component                              `json:"-"`
	state             State                                     `json:"-"`
	mqttClient        *mymqtt.Client                            `json:"-"`
	me                string                                    `json:"-"`
	to                chan<- []byte                             `json:"-"` // channel to send messages to
	from              <-chan []byte                             `json:"-"` // channel to receive messages from
	log               logr.Logger                               `json:"-"`
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

type Config struct {
	BLE       *any                 `json:"ble,omitempty"`
	BtHome    *any                 `json:"bthome,omitempty"`
	Cloud     *any                 `json:"cloud,omitempty"`
	Input0    *sswitch.InputConfig `json:"input:0,omitempty"`
	Input1    *sswitch.InputConfig `json:"input:1,omitempty"`
	Input2    *sswitch.InputConfig `json:"input:2,omitempty"`
	Input3    *sswitch.InputConfig `json:"input:3,omitempty"`
	Knx       *any                 `json:"knx,omitempty"`
	Mqtt      *mqtt.Config         `json:"mqtt,omitempty"`
	Schedule  *schedule.Scheduled  `json:"schedule,omitempty"`
	Switch0   *sswitch.Config      `json:"switch:0,omitempty"`
	Switch1   *sswitch.Config      `json:"switch:1,omitempty"`
	Switch2   *sswitch.Config      `json:"switch:2,omitempty"`
	Switch3   *sswitch.Config      `json:"switch:3,omitempty"`
	System    *system.Config       `json:"system,omitempty"`
	Wifi      *wifi.Config         `json:"wifi,omitempty"`
	WebSocket *any                 `json:"ws,omitempty"`
}

type Status struct {
	BLE       *any                 `json:"ble,omitempty"`
	BtHome    *any                 `json:"bthome,omitempty"`
	Cloud     *any                 `json:"cloud,omitempty"`
	Input0    *sswitch.InputStatus `json:"input:0,omitempty"`
	Input1    *sswitch.InputStatus `json:"input:1,omitempty"`
	Input2    *sswitch.InputStatus `json:"input:2,omitempty"`
	Input3    *sswitch.InputStatus `json:"input:3,omitempty"`
	Knx       *any                 `json:"knx,omitempty"`
	Mqtt      *mqtt.Status         `json:"mqtt,omitempty"`
	Switch0   *sswitch.Status      `json:"switch:0,omitempty"`
	Switch1   *sswitch.Status      `json:"switch:1,omitempty"`
	Switch2   *sswitch.Status      `json:"switch:2,omitempty"`
	Switch3   *sswitch.Status      `json:"switch:3,omitempty"`
	System    *system.Status       `json:"system,omitempty"`
	Wifi      *wifi.Status         `json:"wifi,omitempty"`
	WebSocket *any                 `json:"ws,omitempty"`
}

// From https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly#shellygetcomponents
type ComponentsRequest struct {
	Offset      int      `json:"offset,omitempty"`       // Index of the component from which to start generating the result Optional
	Include     []string `json:"include,omitempty"`      // "status" will include the component's status, "config" - the config. The keys are always included. Combination of both (["config", "status"]) to get the full config and status of each component. Optional
	Keys        []string `json:"keys,omitempty"`         // An array of component keys in the format <type> <cid> (for example, boolean:200) which is used to filter the response list. If empty/not provided, all components will be returned. Optional
	DynamicOnly bool     `json:"dynamic_only,omitempty"` // If true, only dynamic components will be returned. Optional
}

type ComponentsResponse struct {
	Components     *[]Component `json:"components"`
	ConfigRevision int          `json:"cfg_revision"` // The current config revision. See SystemGetConfig#ConfigRevision
	Offset         int          `json:"offset"`       // The index of the first component in the list.
	Total          int          `json:"total"`        // Total number of components with all filters applied.
}

type Component struct {
	Key    string      `json:"key"`    // Component's key (in format <type>:<cid>, for example boolean:200)
	Status interface{} `json:"status"` // Component's status, will be omitted if "status" is not specified in the include property.
	Config interface{} `json:"config"` // Component's config, will be omitted if "config" is not specified in the include property.
}

var nameRe = regexp.MustCompile(fmt.Sprintf("^(?P<id>[a-zA-Z0-9]+).%s.local.$", MDNS_SHELLIES))

var hostRe = regexp.MustCompile("^(?P<model>[a-zA-Z0-9]+)-(?P<serial>[A-Z0-9]+).local.$")

var generationRe = regexp.MustCompile("^gen=(?P<generation>[0-9]+)$")

var applicationRe = regexp.MustCompile("^app=(?P<application>[a-zA-Z0-9]+)$")

var versionRe = regexp.MustCompile("^ver=(?P<version>[.0-9]+)$")

func (d *Device) Call(ctx context.Context, ch types.Channel, component string, verb string, params any) any {
	data, err := d.CallE(ctx, ch, component, verb, params)
	if err != nil {
		panic(err)
	}
	return data
}

func (d *Device) MethodHandlerE(c string, v string) (types.MethodHandler, error) {
	var mh types.MethodHandler
	var exists bool

	if c == Shelly {
		mh, exists = registrar.methods[Shelly][v]
		if !exists {
			return types.MethodNotFound, fmt.Errorf("did not find any registrar handler for method: %v.%v", c, v)
		}
	} else {
		found := false
		if comp, exists := d.ComponentsMethods[c]; exists {
			if mh, exists = comp[v]; exists {
				found = true
			}
		}
		if !found {
			return types.MethodNotFound, fmt.Errorf("did not find any device handler for method: %v.%v", c, v)
		}
	}
	return mh, nil
}

func (d *Device) CallE(ctx context.Context, ch types.Channel, comp string, verb string, params any) (any, error) {
	d.init(ctx)
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

func NewHttpDevice(log logr.Logger, ip net.IP) *Device {
	d := &Device{
		Ipv4_: ip,
		Host:  ip.String(),
		state: New,
		log:   log,
	}
	return d
}

func NewMqttDevice(log logr.Logger, id string, mc *mymqtt.Client) *Device {
	d := &Device{
		Id_:        id,
		Host:       fmt.Sprintf("%s.local.", id),
		mqttClient: mc,
		state:      New,
		log:        log,
	}
	return d
}

func (d *Device) init(ctx context.Context) error {
	d.log.Info("Shelly.init", "id", d.Id_, "host", d.Host)

	var via types.Channel
	if d.mqttClient == nil {
		via = types.ChannelHttp
	} else {
		via = types.ChannelMqtt
	}

	var err error
	if d.Id_ == "" {
		di, err := GetRegistrar().CallE(d, via, GetRegistrar().MethodHandler(Shelly, "GetDeviceInfo"), map[string]interface{}{"ident": true})
		if err != nil {
			return err
		}
		d.Info = di.(*DeviceInfo)
		d.log.Info("Shelly.GetDeviceInfo: loaded", "info", *d.Info)
		d.Id_ = d.Info.Id
		d.MacAddress = d.Info.MacAddress
	}

	hostname, err := os.Hostname()
	if err != nil {
		d.log.Error(err, "Unable to get local hostname")
		return err
	}
	d.me = fmt.Sprintf("%s_%s", hostname, d.Id_)
	d.from, err = d.mqttClient.Subscriber(ctx, fmt.Sprintf("%s/rpc", d.me), 1 /*qlen*/)
	if err != nil {
		d.log.Error(err, "Unable to subscribe to device's RPC topic", "device_id", d.Id_)
		return err
	}

	if d.Info == nil {
		di, err := GetRegistrar().CallE(d, via, GetRegistrar().MethodHandler(Shelly, "GetDeviceInfo"), map[string]interface{}{"ident": true})
		if err != nil {
			return err
		}
		d.Info = di.(*DeviceInfo)
		d.log.Info("Shelly.GetDeviceInfo: loaded", "info", *d.Info)
		d.Id_ = d.Info.Id
		d.MacAddress = d.Info.MacAddress
	}

	if d.Components == nil {
		out, err := GetRegistrar().CallE(d, via, GetRegistrar().MethodHandler(Shelly, "GetComponents"), &ComponentsRequest{})
		if err != nil {
			return err
		}
		d.Components = out.(*ComponentsResponse).Components
		d.log.Info("Shelly.GetComponents", "components", *d.Components)
	}

	if d.to == nil {
		topic := fmt.Sprintf("%s/rpc", d.Id_)
		d.to, err = d.mqttClient.Publisher(ctx, topic, 1 /*qlen*/)
		if err != nil {
			d.log.Error(err, "Unable to publish to device's RPC topic", "device_id", d.Id_)
			return err
		}
		// d.to = make(chan []byte, 1 /*qlen*/)
		// go func() {
		// 	for {
		// 		msg := <-d.to
		// 		d.mqttClient.Publish(toDevice, msg)
		// 	}
		// }()
	}

	if d.Methods == nil {
		m, err := GetRegistrar().CallE(d, via, GetRegistrar().MethodHandler(Shelly, "ListMethods"), nil)
		if err != nil {
			d.log.Error(err, "Unable to list device's methods")
			return err
		}

		d.Methods = m.(*MethodsResponse).Methods
		d.log.Info("Shelly.ListMethods", "methods", d.Methods)

		d.ComponentsMethods = make(map[string]map[string]types.MethodHandler)
		for _, m := range d.Methods {
			mi := strings.Split(m, ".")
			c := mi[0] // component
			v := mi[1] // verb
			for component := types.Shelly; component < types.None; component++ {
				if c == component.String() {
					if _, exists := d.ComponentsMethods[c]; !exists {
						d.ComponentsMethods[c] = make(map[string]types.MethodHandler)
					}
					if _, exists := registrar.methods[c]; exists {
						if _, exists := registrar.methods[c][v]; exists {
							d.ComponentsMethods[c][v] = registrar.methods[c][v]
						}
					}
				}
			}
		}
	}

	return nil
}

type Do func(context.Context, logr.Logger, types.Channel, *Device, []string) (any, error)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		cancel()
	}()

	if len(names) > 0 {
		for _, name := range names {
			log.Info("Looking for Shelly device", "name", name)
			var via types.Channel
			var sd *Device
			ip := net.ParseIP(name)
			if ip != nil {
				sd = NewHttpDevice(log, ip)
				via = types.ChannelHttp
			} else {
				sd = NewMqttDevice(log, name, mc)
				via = types.ChannelMqtt
			}
			out, err := do(ctx, log, via, sd, args)
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
			item, err := do(ctx, log, via, device, args)
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
