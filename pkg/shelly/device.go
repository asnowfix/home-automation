package shelly

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"mynet"
	"net"
	"pkg/devices"
	"pkg/shelly/ethernet"
	"pkg/shelly/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"reflect"
	"regexp"
	"strings"

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
	config      *shelly.Config     `json:"-"`
	status      *shelly.Status     `json:"-"`
	mqttReady   bool               `json:"-"`
	replyTo     string             `json:"-"`
	to          chan<- []byte      `json:"-"` // channel to send messages to
	from        <-chan []byte      `json:"-"` // channel to receive messages from
	dialogId    uint32             `json:"-"`
	dialogs     map[uint32]bool    `json:"-"`
	log         logr.Logger        `json:"-"`
	modified    bool               `json:"-"`
}

func (d *Device) Refresh(ctx context.Context, via types.Channel) error {
	if !d.IsMqttReady() {
		_, err := d.initMqtt(ctx)
		if err != nil {
			return fmt.Errorf("unable to init MQTT (%v)", err)
		}
	}
	if d.Id() == "" || d.Mac() == nil {
		err := d.initDeviceInfo(ctx, types.ChannelHttp)
		if err != nil {
			return fmt.Errorf("unable to init device (%v)", err)
		}
	}
	if d.Name() == "" {
		config, err := system.DoGetConfig(ctx, d)
		if err != nil {
			return fmt.Errorf("unable to system.GetDeviceConfig (%v)", err)
		}
		if d.config == nil {
			d.config = &shelly.Config{}
		}
		d.config.System = config
		d.UpdateName(config.Device.Name)
	}
	if !d.IsHttpReady() {
		if d.status == nil {
			d.status = &shelly.Status{}
		}
		ws, err := wifi.DoGetStatus(ctx, via, d)
		d.log.Info("Wifi status", "device", d.Id(), "status", ws, "error", err)
		if err == nil && ws.IP != "" {
			d.status.Wifi = ws
			d.Host_ = ws.IP
			d.UpdateHost(ws.IP)
		}
		es, err := ethernet.DoGetStatus(ctx, via, d)
		d.log.Info("Ethernet status", "device", d.Id(), "status", es, "error", err)
		if err == nil && es.IP != "" {
			d.status.Ethernet = es
			d.UpdateHost(es.IP)
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

	return nil
}

func (d *Device) Manufacturer() string {
	return "Shelly"
}

func (d *Device) Id() string {
	if d.Id_ == "" || d.Id_ == "<nil>" {
		return ""
	}
	return d.Id_
}

func (d *Device) UpdateId(id string) {
	if id == "" || id == "<nil>" || id == d.Id_ || !deviceIdRe.MatchString(id) {
		return
	}
	d.Id_ = id
	d.modified = true
}

func (d *Device) Host() string {
	if d.Host_ == "" || d.Host_ == "<nil>" {
		return ""
	}
	return d.Host_
}

func (d *Device) UpdateHost(host string) {
	if host == "" || host == "<nil>" || host == d.Host_ || net.ParseIP(host) == nil {
		return
	}
	d.Host_ = host
	d.modified = true
}

func (d *Device) Ip() net.IP {
	if d.status != nil {
		if d.status.Ethernet != nil {
			return net.ParseIP(d.status.Ethernet.IP)
		}
		if d.status.Wifi != nil {
			return net.ParseIP(d.status.Wifi.IP)
		}
	}
	return net.ParseIP(d.Host_)
}

func (d *Device) Name() string {
	if d.Name_ == "" || d.Name_ == "<nil>" {
		return ""
	}
	return d.Name_
}

func (d *Device) UpdateName(name string) {
	if name == "" || name == "<nil>" || name == d.Name_ {
		return
	}
	d.Name_ = name
	d.modified = true
}

func (d *Device) Mac() net.HardwareAddr {
	if d.MacAddress_ == "" || d.MacAddress_ == "<nil>" {
		return nil
	}
	mac, err := net.ParseMAC(d.MacAddress_)
	if err != nil {
		d.log.Error(err, "Failed to parse MAC address", "mac", d.MacAddress_)
		d.UpdateMac(nil)
		return nil
	}
	return mac
}

func (d *Device) UpdateMac(mac net.HardwareAddr) {
	if mac.String() == d.MacAddress_ {
		return
	}
	if mac == nil || mac.String() == "" || mac.String() == "<nil>" {
		d.MacAddress_ = ""
	} else {
		d.MacAddress_ = mac.String()
	}
	d.modified = true
}

func (d *Device) Info() *shelly.DeviceInfo {
	return d.info
}

func (d *Device) Config() *shelly.Config {
	return d.config
}

func (d *Device) ConfigRevision() uint32 {
	if d.config == nil {
		return 0
	}
	if d.config.System == nil {
		return 0
	}
	return d.config.System.ConfigRevision
}

func (d *Device) IsModified() bool {
	return d.modified
}

func (d *Device) ResetModified() {
	d.modified = false
}

func (d *Device) IsMqttReady() bool {
	return d.mqttReady
}

func (d *Device) DisableMqtt() {
	d.mqttReady = false
}

func (d *Device) EnableMqtt() {
	d.mqttReady = true
}

func (d *Device) IsHttpReady() bool {
	var ip net.IP
	if d.status != nil {
		if d.status.Ethernet != nil {
			ip = net.ParseIP(d.status.Ethernet.IP)
		} else if d.status.Wifi != nil {
			ip = net.ParseIP(d.status.Wifi.IP)
		}
	} else {
		ip = net.ParseIP(d.Host_)
	}

	if ip == nil {
		d.Host_ = ""
		return false
	}

	d.UpdateHost(ip.String())
	return mynet.IsSameNetwork(d.log, ip) == nil
}

func (d *Device) StartDialog() uint32 {
	d.dialogId++
	d.dialogs[d.dialogId] = true
	return d.dialogId
}

func (d *Device) StopDialog(id uint32) {
	delete(d.dialogs, id)
}

func (d *Device) Channel(via types.Channel) types.Channel {
	switch via {
	case types.ChannelDefault:
		if d.IsMqttReady() {
			return types.ChannelMqtt
		}
		if d.IsHttpReady() {
			return types.ChannelHttp
		}
	case types.ChannelMqtt:
		if d.IsMqttReady() {
			return types.ChannelMqtt
		}
	case types.ChannelHttp:
		if d.IsHttpReady() {
			return types.ChannelHttp
		}
	}
	panic("no channel is usable")
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

	if strings.HasPrefix(method, "Shelly.") {
		mh, err = GetRegistrar().MethodHandlerE(method)
	} else {
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

func NewDeviceFromIp(ctx context.Context, log logr.Logger, ip net.IP) (devices.Device, error) {
	d := &Device{
		Host_: ip.String(),
		log:   log,
	}
	return d.init(ctx)
}

func NewDeviceFromMqttId(ctx context.Context, log logr.Logger, id string) (devices.Device, error) {
	d := &Device{
		Id_: id,
		log: log,
	}
	return d.init(ctx)
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
		log: log,
	}
	return d.init(ctx)
}

func (d *Device) init(ctx context.Context) (devices.Device, error) {
	if d.Id() == "" {
		err := d.initHttp(ctx)
		if err != nil {
			return nil, err
		}
	}
	return d.initMqtt(ctx)
}

func (d *Device) initHttp(ctx context.Context) error {
	if !d.IsHttpReady() {
		return fmt.Errorf("device host is empty: no channel to communicate with HTTP")
	}
	d.initDeviceInfo(ctx, types.ChannelHttp)
	return nil
}

func (d *Device) initMqtt(ctx context.Context) (devices.Device, error) {
	if d.Id() == "" {
		return nil, fmt.Errorf("device id is empty: no channel to communicate")
	}

	mc, err := mymqtt.GetClientE(ctx)
	if err != nil {
		d.log.Error(err, "Unable to get MQTT client")
		return nil, err
	}

	d.replyTo = fmt.Sprintf("%s_%s", mc.Id(), d.Id())
	d.dialogs = make(map[uint32]bool)

	if d.from == nil && mc != nil {
		var err error
		d.from, err = mc.Subscriber(ctx, fmt.Sprintf("%s/rpc", d.replyTo), 1 /*qlen*/)
		if err != nil {
			d.log.Error(err, "Unable to subscribe to device's RPC topic", "device_id", d.Id_)
			return nil, err
		}
	}

	if d.to == nil && mc != nil {
		topic := fmt.Sprintf("%s/rpc", d.Id_)
		var err error
		d.to, err = mc.Publisher(ctx, topic, 1 /*qlen*/)
		if err != nil {
			d.log.Error(err, "Unable to publish to device's RPC topic", "device_id", d.Id_)
			return nil, err
		}
	}

	d.EnableMqtt()
	d.initDeviceInfo(ctx, types.ChannelMqtt)
	return d, nil
}

func (d *Device) initDeviceInfo(ctx context.Context, via types.Channel) error {
	if d.Id() == "" || d.Mac() == nil {
		d.log.Info("Initializing device info", "device_id", d.Id(), "mac", d.Mac())
		out, err := d.CallE(ctx, via, shelly.GetDeviceInfo.String(), nil)
		if err != nil {
			return err
		}
		info, ok := out.(*shelly.DeviceInfo)
		if !ok {
			return fmt.Errorf("invalid response to get device info")
		}
		d.info = info
		d.Id_ = info.Id
		d.MacAddress_ = info.MacAddress.String()
		d.UpdateId(info.Id)
		d.UpdateMac(info.MacAddress)
	}
	return nil
}

func (d *Device) initComponents(ctx context.Context, via types.Channel) error {
	if !d.IsHttpReady() {
		out, err := d.CallE(ctx, via, shelly.GetComponents.String(), &shelly.ComponentsRequest{
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
