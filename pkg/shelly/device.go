package shelly

import (
	"context"
	"devices/shelly/wifi"
	"encoding/json"
	"fmt"
	"mymqtt"
	"net"
	"pkg/shelly/mqtt"
	"pkg/shelly/sswitch"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"reflect"
	"regexp"
	"schedule"

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

type State uint32

type Device struct {
	Product
	Id_        string        `json:"id"`
	Service    string        `json:"service"`
	Host_      string        `json:"host"`
	Port       int           `json:"port"`
	Info       *DeviceInfo   `json:"info"`
	Methods    []string      `json:"methods"`
	Components *[]Component  `json:"-"`
	isMqttOk   bool          `json:"-"`
	me         string        `json:"-"`
	to         chan<- []byte `json:"-"` // channel to send messages to
	from       <-chan []byte `json:"-"` // channel to receive messages from
	log        logr.Logger   `json:"-"`
}

func (d *Device) Id() string {
	return d.Id_
}

func (d *Device) Host() string {
	return d.Host_
}

func (d *Device) SetHost(host string) {
	d.Host_ = host
}

func (d *Device) MqttOk(ok bool) {
	if d.Host_ == "" {
		// No IP=> No other way to reach out to Shelly than MQTT
		d.isMqttOk = true
	} else {
		d.isMqttOk = ok
	}
}

func (d *Device) Channel(via types.Channel) types.Channel {
	if via != types.ChannelDefault {
		return via
	}
	if d.isMqttOk {
		return types.ChannelMqtt
	}
	if d.Host() != "" {
		return types.ChannelHttp
	}
	return types.ChannelDefault
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

// From https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly#shellycheckforupdate

type CheckForUpdateResponse struct {
	Stable *struct {
		Version string `json:"version"`  // The version of the stable firmware
		BuildId string `json:"build_id"` // The build ID of the stable firmware
	} `json:"stable,omitempty"`
	Beta *struct {
		Version string `json:"version"`  // The version of the beta firmware
		BuildId string `json:"build_id"` // The build ID of the beta firmware
	} `json:"beta,omitempty"`
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
	Key    string         `json:"key"`    // Component's key (in format <type>:<cid>, for example boolean:200)
	Config map[string]any `json:"config"` // Component's config, will be omitted if "config" is not specified in the include property.
	// Status  map[string]any                 `json:"status"` // Component's status, will be omitted if "status" is not specified in the include property.
	// Methods map[string]types.MethodHandler `json:"-"`
}

var nameRe = regexp.MustCompile(fmt.Sprintf("^(?P<id>[a-zA-Z0-9]+).%s.local.$", MDNS_SHELLIES))

var hostRe = regexp.MustCompile("^(?P<model>[a-zA-Z0-9]+)-(?P<serial>[A-Z0-9]+).local.$")

var generationRe = regexp.MustCompile("^gen=(?P<generation>[0-9]+)$")

var applicationRe = regexp.MustCompile("^app=(?P<application>[a-zA-Z0-9]+)$")

var versionRe = regexp.MustCompile("^ver=(?P<version>[.0-9]+)$")

func (d *Device) CallE(ctx context.Context, via types.Channel, method string, params any) (any, error) {
	var mh types.MethodHandler
	var err error

	switch reflect.TypeOf(method).PkgPath() {
	case reflect.TypeOf(ListMethods).PkgPath():
		// Shelly.*
		fallthrough
	case reflect.TypeOf(system.GetConfig).PkgPath():
		// Sys.*
		mh, err = registrar.MethodHandlerE(method)
	default:
		err = d.Init(ctx)
		if err != nil {
			d.log.Error(err, "Unable to initialize device", "id", d.Id(), "host", d.Host())
			return nil, err
		}
		err = d.methods(ctx, via)
		if err != nil {
			d.log.Error(err, "Unable to get device's methods", "id", d.Id(), "host", d.Host())
			return nil, err
		}
		mh, err = d.MethodHandlerE(method)

	}
	if err != nil {
		d.log.Error(err, "Unable to find method handler", "method", method)
		return nil, err
	}
	return GetRegistrar().CallE(ctx, d, via, mh, params)
}

func (d *Device) MethodHandlerE(v any) (types.MethodHandler, error) {
	m, ok := v.(string)
	if !ok {
		return types.NotAMethod, fmt.Errorf("not a method: %v", v)
	}
	mh, exists := registrar.methods[m]
	if !exists {
		return types.MethodNotFound, fmt.Errorf("did not find any registrar handler for method: %v", v)
	}
	return mh, nil
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

func NewDeviceFromIp(ctx context.Context, log logr.Logger, ip net.IP) *Device {
	d := &Device{
		Host_:    ip.String(),
		log:      log,
		isMqttOk: false,
	}
	return d
}

func NewDeviceFromMqttId(ctx context.Context, log logr.Logger, id string, mc *mymqtt.Client) *Device {
	d := &Device{
		Id_:      id,
		log:      log,
		isMqttOk: true,
	}
	return d
}

func NewDeviceFromInfo(ctx context.Context, log logr.Logger, info *DeviceInfo) *Device {
	d := &Device{
		Id_:      info.Id,
		Info:     info,
		log:      log,
		isMqttOk: true,
	}
	return d
}

func (d *Device) Init(ctx context.Context) error {
	var err error
	var mc *mymqtt.Client

	if d.Id() == "" && d.Host() == "" {
		return fmt.Errorf("device id & host is empty")
	}

	mc, err = mymqtt.GetClientE(ctx)
	if err != nil {
		d.log.Error(err, "Unable to get MQTT client for the current process")
		return err
	}
	d.me = fmt.Sprintf("%s_%s", mc.Id(), d.Id_)

	if d.from == nil && mc != nil {
		d.from, err = mc.Subscriber(ctx, fmt.Sprintf("%s/rpc", d.me), 1 /*qlen*/)
		if err != nil {
			d.log.Error(err, "Unable to subscribe to device's RPC topic", "device_id", d.Id_)
			return err
		}
	}

	if d.to == nil && mc != nil {
		topic := fmt.Sprintf("%s/rpc", d.Id_)
		d.to, err = mc.Publisher(ctx, topic, 1 /*qlen*/)
		if err != nil {
			d.log.Error(err, "Unable to publish to device's RPC topic", "device_id", d.Id_)
			return err
		}
	}

	if d.Info == nil {
		mh, err := GetRegistrar().MethodHandlerE(GetDeviceInfo.String())
		if err != nil {
			d.log.Error(err, "Unable to get method handler", "method", GetDeviceInfo)
			return err
		}
		di, err := GetRegistrar().CallE(ctx, d, types.ChannelDefault, mh, map[string]any{"ident": true})
		if err != nil {
			d.log.Error(err, "Unable to get device info", "device_id", d.Id_)
			return err
		}
		d.Info = di.(*DeviceInfo)
		d.log.Info("Shelly.GetDeviceInfo: loaded", "info", *d.Info)
		d.Id_ = d.Info.Id
		d.MacAddress = d.Info.MacAddress
	}

	return nil
}

func (d *Device) methods(ctx context.Context, via types.Channel) error {
	if d.Components == nil {
		mh, err := GetRegistrar().MethodHandlerE(GetComponents.String())
		if err != nil {
			d.log.Error(err, "Unable to get method handler", "method", GetComponents)
			return err
		}
		out, err := GetRegistrar().CallE(ctx, d, via, mh, &ComponentsRequest{
			Offset:      0,
			Include:     []string{"config"},
			DynamicOnly: false,
		})
		if err != nil {
			return err
		}
		res, ok := out.(*ComponentsResponse)
		if ok && res.Components != nil {
			d.Components = res.Components
			// d.log.Info("response", "verb", GetComponents, "id", d.Id(), "components", *d.Components)
		} else {
			d.log.Info("Unable to get components", "for device_id", d.Id())
		}
	}

	if d.Components != nil {
		var mqttComponent *Component
		for _, c := range *d.Components {
			if c.Key == "mqtt" {
				mqttComponent = &c
				break
			}
		}

		mc, err := mymqtt.GetClientE(ctx)
		if err != nil {
			d.log.Error(err, "Unable to get MQTT client")
			return err
		}

		if mqttComponent != nil {
			// "mqtt": {
			// 	"client_id": "shellyplus1-08b61fd9d708",
			// 	"enable": true,
			// 	"enable_control": true,
			// 	"rpc_ntf": true,
			// 	"server": "192.168.1.2:1883",
			// 	"status_ntf": true,
			// 	"topic_prefix": "shellyplus1-08b61fd9d708",
			// 	"use_client_cert": false
			// }
			brokerUrl := mc.BrokerUrl().Host

			if mqttComponent.Config == nil || mqttComponent.Config["server"].(string) != brokerUrl {
				d.log.Info("Configuring MQTT", "broker", brokerUrl)
				mh, err := GetRegistrar().MethodHandlerE(mqtt.SetConfig.String())
				if err != nil {
					d.log.Error(err, "Unable to get method handler", "method", mqtt.SetConfig)
					return err
				}
				_, err = GetRegistrar().CallE(ctx, d, via, mh, &mqtt.Config{
					Enable:        true,
					Server:        brokerUrl,
					RpcNotifs:     true,
					StatusNotifs:  true,
					EnableControl: true,
				})
				if err != nil {
					d.log.Error(err, "Unable to set MQTT config")
					return err
				}
			}
		}
	}

	if d.Methods == nil {
		mh, err := GetRegistrar().MethodHandlerE(ListMethods.String())
		if err != nil {
			d.log.Error(err, "Unable to get method handler", "method", ListMethods)
			return err
		}
		m, err := GetRegistrar().CallE(ctx, d, via, mh, nil)
		if err != nil {
			d.log.Error(err, "Unable to list device's methods")
			return err
		}

		// TODO: implement dynamic method binding
		d.Methods = m.(*MethodsResponse).Methods
		// d.log.Info("Shelly.ListMethods", "methods", d.Methods)

		// for _, method := range d.Methods {
		// 	cn := strings.SplitN(method, ".", 2)[0]
		// 	if c, exists := d.Components[cn]; !exists {
		// 		return fmt.Errorf("component not found: %s", cn)
		// 	}
		// 	c.Methods = make(map[string]types.MethodHandler)
		// 	for _, m := range d.Methods {
		// 		d.ComponentsMethods[m] = registrar.methods[m]
		// 	}
		// }
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

func Foreach(ctx context.Context, log logr.Logger, mc *mymqtt.Client, names []string, via types.Channel, do Do, args []string) error {
	log.Info("Running", "func", reflect.TypeOf(do), "args", args)
	if len(names) > 0 {
		for _, name := range names {
			log.Info("Looking for Shelly device", "name", name)
			var via types.Channel
			var sd *Device
			ip := net.ParseIP(name)
			if ip != nil {
				sd = NewDeviceFromIp(ctx, log, ip)
				via = types.ChannelHttp
			} else {
				sd = NewDeviceFromMqttId(ctx, log, name, mc)
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
