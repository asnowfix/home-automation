package shelly

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"net"
	"pkg/devices"
	"pkg/shelly/ethernet"
	"pkg/shelly/mqtt"
	"pkg/shelly/sswitch"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
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
	Id_      string        `json:"id"`
	Name_    string        `json:"name"`
	Service  string        `json:"service"`
	Host_    string        `json:"host"`
	Port     int           `json:"port"`
	info     *DeviceInfo   `json:"-"`
	Methods  []string      `json:"-"`
	config   *Config       `json:"-"`
	status   *Status       `json:"-"`
	isMqttOk bool          `json:"-"`
	replyTo  string        `json:"-"`
	to       chan<- []byte `json:"-"` // channel to send messages to
	from     <-chan []byte `json:"-"` // channel to receive messages from
	log      logr.Logger   `json:"-"`
}

func (d *Device) Manufacturer() string {
	return "Shelly"
}

func (d *Device) Id() string {
	return d.Id_
}

func (d *Device) Host() string {
	return d.Host_
}

func (d *Device) Ip() net.IP {
	if d.status == nil || (d.status.Wifi == nil && d.status.Ethernet == nil) {
		return nil
	}
	if d.status.Wifi != nil {
		return net.ParseIP(d.status.Wifi.IP)
	}
	if d.status.Ethernet != nil {
		return net.ParseIP(d.status.Ethernet.Ip)
	}
	return nil
}

func (d *Device) Name() string {
	if d.config == nil || d.config.System == nil || d.config.System.Device == nil {
		return ""
	}
	return d.config.System.Device.Name
}

func (d *Device) Mac() net.HardwareAddr {
	// FIXME: put a device update on the backburner
	if d.info == nil || d.info.Product == nil || d.info.Product.MacAddress == nil {
		return nil
	}
	return d.info.Product.MacAddress
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
	Ethernet  *ethernet.Config     `json:"eth,omitempty"`
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
	Ethernet  *ethernet.Status     `json:"eth,omitempty"`
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

type Components struct {
	Config *Config `json:"-"`
	Status *Status `json:"-"`
}

type ComponentsResponse struct {
	Components
	Response_      *[]ComponentResponse `json:"components"`
	ConfigRevision int                  `json:"cfg_revision"` // The current config revision. See SystemGetConfig#ConfigRevision
	Offset         int                  `json:"offset"`       // The index of the first component in the list.
	Total          int                  `json:"total"`        // Total number of components with all filters applied.
}

func (cr *ComponentsResponse) UnmarshalJSON(data []byte) error {
	type Alias ComponentsResponse
	if err := json.Unmarshal(data, (*Alias)(cr)); err != nil {
		return err
	}
	if cr.Response_ == nil {
		cr.Response_ = &[]ComponentResponse{}
	}
	config := make(map[string]any)
	status := make(map[string]any)

	for _, comp := range *cr.Response_ {
		config[comp.Key] = comp.Config
		status[comp.Key] = comp.Status
	}

	configStr, err := json.Marshal(config)
	if err != nil {
		return err
	}
	cr.Config = &Config{}
	if err := json.Unmarshal(configStr, cr.Config); err != nil {
		return err
	}
	statusStr, err := json.Marshal(status)
	if err != nil {
		return err
	}
	cr.Status = &Status{}
	if err := json.Unmarshal(statusStr, cr.Status); err != nil {
		return err
	}
	return nil
}

type ComponentResponse struct {
	Key    string         `json:"key"`    // Component's key (in format <type>:<cid>, for example boolean:200)
	Config map[string]any `json:"config"` // Component's config, will be omitted if "config" is not specified in the include property.
	Status map[string]any `json:"status"` // Component's status, will be omitted if "status" is not specified in the include property.
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
	name := d.Name()
	if len(name) == 0 {
		return d.Id()
	}

	return fmt.Sprintf("%s (%s)", name, d.Id())
}

func (d *Device) To() chan<- []byte {
	return d.to
}

func (d *Device) From() <-chan []byte {
	return d.from
}

func (d *Device) ReplyTo() string {
	return d.replyTo
}

// func NewDeviceFromIp(ctx context.Context, log logr.Logger, ip net.IP) *Device {
// 	d := &Device{
// 		Host_:    ip.String(),
// 		log:      log,
// 		isMqttOk: false,
// 	}
// 	return d
// }

func NewDeviceFromMqttId(ctx context.Context, log logr.Logger, id string) (*Device, error) {
	d := &Device{
		Id_:      id,
		log:      log,
		isMqttOk: true,
	}
	err := d.init(ctx)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func NewDeviceFromSummary(ctx context.Context, log logr.Logger, summary devices.Device) (devices.Device, error) {
	d := &Device{
		Id_:   summary.Id(),
		Name_: summary.Name(),
		Host_: summary.Host(),
		info: &DeviceInfo{
			Id: summary.Id(),
			Product: &Product{
				MacAddress: summary.Mac(),
			},
		},
		log:      log,
		isMqttOk: true,
	}
	err := d.init(ctx)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Device) init(ctx context.Context) error {
	mc, err := mymqtt.GetClientE(ctx)
	if err != nil {
		d.log.Error(err, "Unable to get MQTT client")
		return err
	}

	if d.Id() == "" && d.Host() == "" {
		return fmt.Errorf("device id & host are empty")
	}

	d.replyTo = fmt.Sprintf("%s_%s", mc.Id(), d.Id())

	if d.from == nil && mc != nil {
		var err error
		d.from, err = mc.Subscriber(ctx, fmt.Sprintf("%s/rpc", d.replyTo), 1 /*qlen*/)
		if err != nil {
			d.log.Error(err, "Unable to subscribe to device's RPC topic", "device_id", d.Id_)
			return err
		}
	}

	if d.to == nil && mc != nil {
		topic := fmt.Sprintf("%s/rpc", d.Id_)
		var err error
		d.to, err = mc.Publisher(ctx, topic, 1 /*qlen*/)
		if err != nil {
			d.log.Error(err, "Unable to publish to device's RPC topic", "device_id", d.Id_)
			return err
		}
	}

	return nil
}

func (d *Device) Load(ctx context.Context) error {
	d.log.Info("Loading device", "id", d.Id(), "host", d.Host())
	if d.MacAddress == nil {
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

		var ok bool
		d.info, ok = di.(*DeviceInfo)
		if !ok {
			d.log.Error(err, "Unable to get device info", "device_id", d.Id_)
			return err
		}
		d.log.Info("Shelly.GetDeviceInfo: got", "info", *d.info)

		if d.info.Id == "" || len(d.info.MacAddress) == 0 {
			err = fmt.Errorf("invalid device info: ignoring:%v", *d.info)
			return err
		}
		d.Id_ = d.info.Id
		d.MacAddress = d.info.MacAddress
	}

	if d.Host() == "" || d.Host() == "<nil>" || d.Ip() == nil {
		out, err := d.CallE(ctx, types.ChannelDefault, GetComponents.String(), &ComponentsRequest{
			Keys: []string{"config", "status"},
		})
		if err != nil {
			d.log.Error(err, "Unable to get device's components", "device_id", d.Id_)
			return err
		}
		cr, ok := out.(*ComponentsResponse)
		if !ok {
			d.log.Error(err, "Unable to get device's components", "device_id", d.Id_)
			return err
		}
		d.log.Info("Shelly.GetComponents: got", "components", *cr)
		d.status = cr.Status
		d.config = cr.Config
	}

	return nil
}

func (d *Device) methods(ctx context.Context, via types.Channel) error {
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

type Do func(context.Context, logr.Logger, types.Channel, devices.Device, []string) (any, error)

func Print(log logr.Logger, d any) error {
	buf, err := json.Marshal(d)
	if err != nil {
		log.Error(err, "Unable to JSON-ify", "out", d)
		return err
	}
	fmt.Print(string(buf))
	return nil
}

func Foreach(ctx context.Context, log logr.Logger, devices []devices.Device, via types.Channel, do Do, args []string) (any, error) {
	out := make([]any, 0, len(devices))
	log.Info("Running", "func", reflect.TypeOf(do), "args", args)

	for _, device := range devices {
		device, err := NewDeviceFromSummary(ctx, log, device)
		if err != nil {
			log.Error(err, "Unable to create device from summary", "device", device)
			return nil, err
		}
		one, err := do(ctx, log, via, device, args)
		out = append(out, one)
		if err != nil {
			log.Error(err, "Operation failed device", "id", device.Id(), "name", device.Name(), "ip", device.Ip())
			return nil, err
		}
	}

	return out, nil
}
