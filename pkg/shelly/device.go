package shelly

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"net"
	"pkg/devices"
	"pkg/shelly/ethernet"
	"pkg/shelly/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"reflect"
	"regexp"

	"github.com/go-logr/logr"
)

type Device struct {
	shelly.Product
	Id_         string             `json:"id"`
	MacAddress_ string             `json:"mac"`
	Name_       string             `json:"name"`
	Service     string             `json:"service"`
	Host_       string             `json:"host"`
	Port        int                `json:"port"`
	info        *shelly.DeviceInfo `json:"-"`
	Methods     []string           `json:"-"`
	config      *shelly.Config     `json:"-"`
	status      *shelly.Status     `json:"-"`
	isMqttOk    bool               `json:"-"`
	replyTo     string             `json:"-"`
	to          chan<- []byte      `json:"-"` // channel to send messages to
	from        <-chan []byte      `json:"-"` // channel to receive messages from
	log         logr.Logger        `json:"-"`
}

func (d *Device) Refresh(ctx context.Context, via types.Channel) (bool, error) {
	updated := false
	if d.Id() == "" || d.Id() == "<nil>" || d.Mac() == nil {
		info, err := shelly.DoGetDeviceInfo(ctx, d)
		if err != nil {
			return false, fmt.Errorf("Unable to shelly.GetDeviceInfo (%v)", err)
		}
		d.info = info
		d.Id_ = info.Id
		d.MacAddress_ = info.MacAddress.String()
		updated = true
	}
	if d.Name() == "" || d.Name() == "<nil>" {
		config, err := system.DoGetConfig(ctx, d)
		if err != nil {
			return false, fmt.Errorf("Unable to system.GetDeviceConfig (%v)", err)
		}
		d.config.System = config
		d.Name_ = config.Device.Name
		updated = true
	}
	if d.Host() == "" || d.Host() == "<nil>" {
		ws, err := wifi.DoGetStatus(ctx, via, d)
		if err == nil && ws.IP != "" {
			d.status.Wifi = ws
			d.Host_ = ws.IP
			updated = true
		}
		es, err := ethernet.DoGetStatus(ctx, via, d)
		if err == nil && es.IP != "" {
			d.status.Ethernet = es
			d.Host_ = es.IP
			updated = true
		}
	}

	// if d.components == nil {
	// 	out, err := d.CallE(ctx, via, shelly.GetComponents.String(), &shelly.ComponentsRequest{
	// 		Keys: []string{"config", "status"},
	// 	})
	// 	if err != nil {
	// 		d.log.Error(err, "Unable to get device's components (continuing)")
	// 	} else {
	// 		crs, ok := out.(*shelly.ComponentsResponse)
	// 		if ok && crs != nil {
	// 			updated = true
	// 		} else {
	// 			d.log.Error(err, "Invalid response to get device's components (continuing)", "response", out)
	// 		}
	// 	}
	// }

	// if d.ConfigRevision == 0 || d.Name() == "" {
	// 	out, err := sd.CallE(ctx, via, system.GetConfig.String(), nil)
	// 	if err != nil {
	// 		d.log.Error(err, "Unable to get device system config (continuing)")
	// 	} else {
	// 		sc, ok := out.(*system.Config)
	// 		if ok && sc != nil && sc.Device != nil {
	// 			d.Name_ = sc.Device.Name
	// 			d.ConfigRevision = sc.ConfigRevision
	// 			// d.SetComponentStatus("system", nil, *sc) FIXME
	// 			updated = true
	// 		} else {
	// 			d.log.Error(err, "Invalid response to get device system config (continuing)", "response", out)
	// 		}
	// 	}
	// }

	d.log.Info("Device update", "device", d, "updated", updated)

	return updated, nil
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
		return net.ParseIP(d.status.Ethernet.IP)
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
	if d.isMqttOk && d.Id() != "" {
		return types.ChannelMqtt
	}
	if d.Host() != "" {
		return types.ChannelHttp
	}
	return types.ChannelDefault
}

var nameRe = regexp.MustCompile(fmt.Sprintf("^(?P<id>[a-zA-Z0-9]+).%s.local.$", MDNS_SHELLIES))

var hostRe = regexp.MustCompile("^(?P<model>[a-zA-Z0-9]+)-(?P<serial>[A-Z0-9]+).local.$")

var generationRe = regexp.MustCompile("^gen=(?P<generation>[0-9]+)$")

var applicationRe = regexp.MustCompile("^app=(?P<application>[a-zA-Z0-9]+)$")

var versionRe = regexp.MustCompile("^ver=(?P<version>[.0-9]+)$")

var deviceIdRe = regexp.MustCompile("^shelly[a-zA-Z0-9]+-[a-f0-9]{12}$")

func (d *Device) CallE(ctx context.Context, via types.Channel, method string, params any) (any, error) {
	var mh types.MethodHandler
	var err error

	switch reflect.TypeOf(method).PkgPath() {
	case reflect.TypeOf(shelly.ListMethods).PkgPath():
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

func NewDeviceFromIp(ctx context.Context, log logr.Logger, ip net.IP) *Device {
	d := &Device{
		Host_:    ip.String(),
		log:      log,
		isMqttOk: false,
	}
	return d
}

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
		info: &shelly.DeviceInfo{
			Id: summary.Id(),
			Product: &shelly.Product{
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
	if d.Id() == "" && d.Host() == "" {
		return fmt.Errorf("device id & host are empty")
	}

	if d.Id() == "" {
		return nil
	}

	mc, err := mymqtt.GetClientE(ctx)
	if err != nil {
		d.log.Error(err, "Unable to get MQTT client")
		return err
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
	if d.Id() != "" {
		d.init(ctx)
	}
	if d.MacAddress_ == "" || d.MacAddress_ == "<nil>" {
		mh, err := GetRegistrar().MethodHandlerE(shelly.GetDeviceInfo.String())
		if err != nil {
			d.log.Error(err, "Unable to get method handler", "method", shelly.GetDeviceInfo)
			return err
		}
		di, err := GetRegistrar().CallE(ctx, d, d.Channel(types.ChannelDefault), mh, map[string]any{"ident": true})
		if err != nil {
			d.log.Error(err, "Unable to get device info", "device_id", d.Id_)
			return err
		}

		var ok bool
		d.info, ok = di.(*shelly.DeviceInfo)
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
		d.MacAddress_ = d.info.MacAddress.String()
		d.isMqttOk = true
		d.init(ctx)
	}

	if d.Host() == "" || d.Host() == "<nil>" || d.Ip() == nil { // XXX avoid <nil>
		out, err := d.CallE(ctx, types.ChannelDefault, shelly.GetComponents.String(), &shelly.ComponentsRequest{
			Keys: []string{"config", "status"},
		})
		if err != nil {
			d.log.Error(err, "Unable to get device's components", "device_id", d.Id_)
			return err
		}
		cr, ok := out.(*shelly.ComponentsResponse)
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
		mh, err := GetRegistrar().MethodHandlerE(shelly.ListMethods.String())
		if err != nil {
			d.log.Error(err, "Unable to get method handler", "method", shelly.ListMethods)
			return err
		}
		m, err := GetRegistrar().CallE(ctx, d, via, mh, nil)
		if err != nil {
			d.log.Error(err, "Unable to list device's methods")
			return err
		}

		// TODO: implement dynamic method binding
		d.Methods = m.(*shelly.MethodsResponse).Methods
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
