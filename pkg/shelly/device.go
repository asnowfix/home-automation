package shelly

import (
	"context"
	"encoding/json"
	"fmt"
	"mynet"
	"net"
	"pkg/devices"
	"pkg/shelly/ethernet"
	"pkg/shelly/mqtt"
	"pkg/shelly/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
	"tools"

	"github.com/go-logr/logr"
)

type Device struct {
	Id_         string               `json:"id"`
	MacAddress_ net.HardwareAddr     `json:"-"`
	Name_       string               `json:"name"`
	Host_       net.IP               `json:"host"`
	info        *shelly.DeviceInfo   `json:"-"`
	config      *shelly.Config       `json:"-"`
	status      *shelly.Status       `json:"-"`
	replyTo     string               `json:"-"`
	to          chan<- []byte        `json:"-"` // channel to send messages to
	from        <-chan []byte        `json:"-"` // channel to receive messages from
	dialogId    uint32               `json:"-"`
	dialogs     sync.Map             `json:"-"` // map[uint32]bool
	log         logr.Logger          `json:"-"`
	modified    bool                 `json:"-"`
	mutex       tools.ReentrantMutex `json:"-"`
}

func (d *Device) Refresh(ctx context.Context, via types.Channel) (bool, error) {
	d.mutex.Lock(ctx)
	defer d.mutex.Unlock(ctx)

	// Gen1 devices (like shellyht-*) cannot be refreshed via RPC
	if strings.HasPrefix(d.Id(), "shellyht-") {
		d.log.V(1).Info("Skipping refresh for Gen1 device", "device_id", d.Id())
		return false, nil
	}

	if !d.IsMqttReady() && d.Id() != "" {
		err := d.initMqtt(ctx)
		if err != nil {
			return false, fmt.Errorf("unable to init MQTT (%v)", err)
		}
	}
	if d.Id() == "" {
		err := d.initDeviceInfo(ctx, via)
		if err != nil {
			return d.IsModified(), fmt.Errorf("unable to init device (%v) using HTTP", err)
		}
	}
	if d.Mac() == nil {
		err := d.initDeviceInfo(ctx, via)
		if err != nil {
			return d.IsModified(), fmt.Errorf("unable to init device (%v)", err)
		}
	}
	if d.Name() == "" {
		config, err := system.GetConfig(ctx, via, d)
		if err != nil {
			return d.IsModified(), fmt.Errorf("unable to system.GetDeviceConfig (%v)", err)
		}
		if d.config == nil {
			d.config = &shelly.Config{}
		}
		d.config.System = config
		if config.Device.Name != "" {
			d.UpdateName(config.Device.Name)
		}
	}
	if !d.IsHttpReady() {
		if d.status == nil {
			d.status = &shelly.Status{}
		}
		ws, err := wifi.DoGetStatus(ctx, via, d)
		d.log.Info("Wifi status", "device", d.Id(), "status", ws, "error", err)
		if err == nil && ws.IP != "" {
			d.status.Wifi = ws
			d.Host_ = net.ParseIP(ws.IP)
			d.UpdateHost(ws.IP)
		}
		es, err := ethernet.DoGetStatus(ctx, via, d)
		d.log.Info("Ethernet status", "device", d.Id(), "status", es, "error", err)
		if err == nil && es.IP != "" {
			d.status.Ethernet = es
			d.UpdateHost(es.IP)
		}
		d.log.Info("Will use IP", "device", d.Id(), "ip", d.Host())
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

	return d.IsModified(), nil
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

	if id == "" || id == "<nil>" || !deviceIdRe.MatchString(id) {
		panic("invalid device id: " + id)
	}
	if d.Id_ == id {
		return
	}
	d.Id_ = id
	d.modified = true
}

func (d *Device) Host() string {
	if d.Host_ == nil {
		return ""
	}
	return d.Host_.String()
}

func (d *Device) UpdateHost(host string) {
	if host == "" || host == "<nil>" {
		return
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return
	}
	if !ip.Equal(d.Host_) {
		d.modified = true
		d.Host_ = ip
	}
}

func (d *Device) Ip() net.IP {
	if d.status != nil {
		if d.status.Ethernet != nil {
			d.Host_ = net.ParseIP(d.status.Ethernet.IP)
		} else if d.status.Wifi != nil {
			d.Host_ = net.ParseIP(d.status.Wifi.IP)
		}
	}
	return d.Host_
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
	return d.MacAddress_
}

func (d *Device) UpdateMac(mac string) {
	if mac == d.MacAddress_.String() {
		return
	}
	if mac == "" || mac == "<nil>" {
		panic("invalid MAC address: " + mac)
	} else {
		if len(mac) == 12 {
			mac = fmt.Sprintf("%s:%s:%s:%s:%s:%s", mac[0:2], mac[2:4], mac[4:6], mac[6:8], mac[8:10], mac[10:12])
		}
		addr, err := net.ParseMAC(mac)
		if err != nil {
			d.log.Error(err, "Failed to parse MAC address", "mac", mac)
			return
		}
		d.MacAddress_ = addr
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
	if d.to == nil {
		return false
	}
	if d.from == nil {
		return false
	}
	if d.Id() == "" {
		return false
	}
	if d.replyTo == "" {
		return false
	}
	return true
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
		ip = d.Host_
	}

	if ip == nil {
		d.log.Info("Device has no IP address", "device", d)
		return false
	}
	d.UpdateHost(ip.String())
	return mynet.IsSameNetwork(d.log, ip) == nil
}

func (d *Device) StartDialog(ctx context.Context) uint32 {
	d.mutex.Lock(ctx)
	defer d.mutex.Unlock(ctx)

	d.dialogId++
	d.dialogs.Store(d.dialogId, true)
	return d.dialogId
}

func (d *Device) StopDialog(ctx context.Context, id uint32) {
	d.mutex.Lock(ctx)
	defer d.mutex.Unlock(ctx)
	d.dialogs.Delete(id)
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
	// Auto discarded
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

	d.mutex.Lock(ctx)
	defer d.mutex.Unlock(ctx)

	if strings.HasPrefix(method, "Shelly.") {
		mh, err = GetRegistrar().MethodHandlerE(method)
	} else {
		mh, err = d.MethodHandlerE(method)
	}
	if err != nil {
		d.log.Error(err, "Unable to find method handler", "method", method)
		return nil, err
	}
	// FIXME: rather use per-device flow-controlled Channel
	time.Sleep(200 * time.Millisecond)

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
		return fmt.Sprintf("%s [%v]", d.Id(), d.Ip())
	} else {
		return fmt.Sprintf("%s (%s) [%v]", d.Id(), name, d.Ip())
	}
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
		Host_: ip,
		log:   log,
	}
	err := d.init(ctx)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func NewDeviceFromMqttId(ctx context.Context, log logr.Logger, id string) (devices.Device, error) {
	if id == "" || id == "<nil>" {
		return nil, fmt.Errorf("invalid device id: %s", id)
	}
	d := &Device{
		log: log,
	}
	d.UpdateId(id)
	return d, nil
}

func NewDeviceFromSummary(ctx context.Context, log logr.Logger, summary devices.Device) (devices.Device, error) {
	d := &Device{
		// info: &shelly.DeviceInfo{
		// 	Name:    summary.Name(),
		// 	Id:      summary.Id(),
		// 	Product: shelly.Product{},
		// },
		log: log,
	}
	d.UpdateId(summary.Id())
	d.UpdateHost(summary.Host())
	d.UpdateMac(summary.Mac().String())
	d.UpdateName(summary.Name())

	return d, nil
}

func (d *Device) init(ctx context.Context) error {
	if d.Id() == "" {
		err := d.initHttp(ctx)
		if err != nil {
			return err
		}
		if d.info == nil {
			panic("info is nil")
		}
	}
	return d.initMqtt(ctx)
}

func (d *Device) initHttp(ctx context.Context) error {
	if d == nil {
		panic("device is nil")
	}
	if !d.IsHttpReady() {
		return fmt.Errorf("device host is empty: no channel to communicate with HTTP")
	}
	d.initDeviceInfo(ctx, types.ChannelHttp)
	return nil
}

func (d *Device) initMqtt(ctx context.Context) error {
	if d == nil {
		panic("device is nil")
	}

	if d.Id() == "" {
		panic("device id is empty: no channel to communicate")
		// return fmt.Errorf("device id is empty: no channel to communicate")
	}

	mc, err := mqtt.FromContext(ctx)
	if err != nil {
		d.log.Error(err, "Unable to get MQTT client")
		return err
	}

	d.replyTo = fmt.Sprintf("%s_%s", mc.Id(), d.Id())
	// dialogs sync.Map doesn't need initialization

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

	d.initDeviceInfo(ctx, types.ChannelMqtt)
	return nil
}

func (d *Device) initDeviceInfo(ctx context.Context, via types.Channel) error {
	if d == nil {
		panic("device is nil")
	}
	if d.Id() == "" || d.Mac() == nil {
		info, err := shelly.GetDeviceInfo(ctx, d, via)
		if err != nil {
			return err
		}
		d.info = info
		d.UpdateMac(info.MacAddress)
		d.UpdateId(info.Id)
		if info.Name != nil {
			d.UpdateName(*info.Name)
		}
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

// DeviceResult represents the result of an operation on a single device
type DeviceResult struct {
	Device devices.Device
	Result any
	Error  error
}

func Foreach(ctx context.Context, log logr.Logger, deviceList []devices.Device, via types.Channel, do Do, args []string) (any, error) {
	log.Info("Running", "func_type", reflect.TypeOf(do), "args", args, "nb_devices", len(deviceList))

	// Create channels for results
	results := make(chan DeviceResult, len(deviceList))
	var wg sync.WaitGroup

	// Process each device in parallel
	for _, dev := range deviceList {
		wg.Add(1)
		go func(devSummary devices.Device) {
			defer wg.Done()

			// Create device from summary
			device, err := NewDeviceFromSummary(ctx, log, devSummary)
			if err != nil {
				log.Error(err, "Unable to create device from summary", "device", devSummary)
				results <- DeviceResult{Device: devSummary, Error: err}
				return
			}

			// Initialize device
			sd, ok := device.(*Device)
			if !ok {
				err := fmt.Errorf("invalid device type %T", device)
				log.Error(nil, "Invalid device type", "type", reflect.TypeOf(device))
				results <- DeviceResult{Device: devSummary, Error: err}
				return
			}
			err = sd.init(ctx)
			if err != nil {
				log.Error(err, "Unable to init device", "device", device)
				results <- DeviceResult{Device: devSummary, Error: err}
				return
			}

			// Execute operation
			one, err := do(ctx, log, via, device, args)
			if err != nil {
				log.Error(err, "Operation failed for device", "id", device.Id(), "name", device.Name(), "ip", device.Ip())
				results <- DeviceResult{Device: device, Result: one, Error: err}
				return
			}

			results <- DeviceResult{Device: device, Result: one, Error: nil}
		}(dev)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	out := make([]any, 0, len(deviceList))
	var errs []error
	type failedDevice struct {
		name string
		id   string
	}
	var failedDevices []failedDevice
	for result := range results {
		if result.Error != nil {
			deviceId := result.Device.Id()
			deviceName := result.Device.Name()
			errs = append(errs, fmt.Errorf("device %s: %w", deviceId, result.Error))
			failedDevices = append(failedDevices, failedDevice{name: deviceName, id: deviceId})
		}
		out = append(out, result.Result)
	}

	// If any devices failed, report them
	if len(errs) > 0 {
		successCount := len(deviceList) - len(errs)

		// Print summary to stdout
		fmt.Printf("\n")
		if successCount > 0 {
			fmt.Printf("✓ %d device(s) succeeded\n", successCount)
		}
		fmt.Printf("✗ %d device(s) failed:\n", len(errs))
		for _, dev := range failedDevices {
			fmt.Printf("  - %s (%s)\n", dev.name, dev.id)
		}

		// Log details
		log.Info("Operation completed with errors", "succeeded", successCount, "failed", len(errs), "total", len(deviceList))

		// If all devices failed, return error
		if len(errs) == len(deviceList) {
			return out, fmt.Errorf("all %d device(s) failed", len(deviceList))
		}

		// If some devices failed, return aggregated error
		return out, fmt.Errorf("%d of %d device(s) failed", len(errs), len(deviceList))
	}

	return out, nil
}
