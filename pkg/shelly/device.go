package shelly

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/ratelimit"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/system"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
)

// DeviceMqttChannels holds MQTT channels for each device ID to prevent goroutine leaks.
// When Device structs are recreated (e.g., loaded from database), they reuse existing channels
// instead of creating new subscriptions/publishers that would leak goroutines.
type DeviceMqttChannels struct {
	ReplyTo string
	To      chan<- []byte
	From    <-chan []byte
	mu      sync.Mutex // Serializes MQTT request-response cycles to prevent ID mismatch
}

func (m *DeviceMqttChannels) IsReady() bool {
	if m == nil {
		return false
	}
	if m.To == nil {
		return false
	}
	if m.From == nil {
		return false
	}
	if m.ReplyTo == "" {
		return false
	}
	return true
}

// Lock acquires the mutex to serialize MQTT request-response cycles
func (m *DeviceMqttChannels) Lock() {
	if m != nil {
		m.mu.Lock()
	}
}

// Unlock releases the mutex
func (m *DeviceMqttChannels) Unlock() {
	if m != nil {
		m.mu.Unlock()
	}
}

// Init initializes MQTT channels for a device. Returns an existing instance from the registry
// if available, or creates new channels and registers them.
func (m *DeviceMqttChannels) Init(ctx context.Context, deviceId string) (*DeviceMqttChannels, error) {
	// Check if we already have MQTT channels for this device (prevents goroutine leaks)
	deviceMqttRegistryMutex.RLock()
	existing, exists := deviceMqttRegistry[deviceId]
	deviceMqttRegistryMutex.RUnlock()

	if exists {
		return existing, nil
	}

	// Create new channels (first time for this device)
	deviceMqttRegistryMutex.Lock()
	defer deviceMqttRegistryMutex.Unlock()

	// Double-check after acquiring write lock
	if existing, exists = deviceMqttRegistry[deviceId]; exists {
		return existing, nil
	}

	mc := mqtt.GetClient(ctx)
	replyTo := fmt.Sprintf("%s_%s", mc.Id(), deviceId)

	// Use larger buffer (64) to handle concurrent refresh operations without dropping responses
	// With maxConcurrentRefreshes and multiple devices, responses can arrive in bursts
	from, err := mc.Subscribe(ctx, fmt.Sprintf("%s/rpc", replyTo), 64 /*qlen*/, "shelly/device/"+deviceId)
	if err != nil {
		return nil, fmt.Errorf("unable to subscribe to device's RPC topic: %w", err)
	}

	topic := fmt.Sprintf("%s/rpc", deviceId)
	to, err := mc.Publisher(ctx, topic, 1 /*qlen*/, mqtt.ExactlyOnce, false /*retain*/, "shelly/device/"+deviceId)
	if err != nil {
		return nil, fmt.Errorf("unable to publish to device's RPC topic: %w", err)
	}

	// Store in registry for future reuse
	channels := &DeviceMqttChannels{
		ReplyTo: replyTo,
		To:      to,
		From:    from,
	}
	deviceMqttRegistry[deviceId] = channels
	return channels, nil
}

var (
	// Global registry of MQTT channels per device ID
	deviceMqttRegistry      = make(map[string]*DeviceMqttChannels)
	deviceMqttRegistryMutex sync.RWMutex
)

// Summary is the minimal, serializable identity of a device — the shape a
// caller already has on hand (e.g. a database row or an mDNS browse result)
// before a live *Device is created for it. It deliberately excludes any
// transport/session concern (CallE, MQTT channels, dialogs): those exist
// only on *Device once initialized.
//
// Declared locally so pkg/shelly does not depend on the app's pkg/devices
// package (see CLAUDE.md's Three-Tier Layer Rule): any type with these six
// methods — including pkg/devices.Device — already satisfies it structurally.
type Summary interface {
	Manufacturer() string
	Id() string
	Name() string
	Host() string
	Ip() net.IP
	Mac() net.HardwareAddr
}

type Device struct {
	id         string
	macAddress net.HardwareAddr
	name       string
	host       net.IP
	info       *shelly.DeviceInfo
	config     *shelly.Config
	status     *shelly.Status
	mqtt       *DeviceMqttChannels // MQTT channels (shared via registry, includes mutex for serialization)
	dialogId   uint32
	dialogs    sync.Map // map[uint32]bool
	log        logr.Logger
	modified   bool
}

// deviceJSON is Device's wire format. Only the identity fields that were
// historically exported (as Id_, Name_, Host_) round-trip; the MAC address
// was already excluded from JSON, and the transport/cache fields never were.
type deviceJSON struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Host net.IP `json:"host"`
}

func (d *Device) MarshalJSON() ([]byte, error) {
	return json.Marshal(deviceJSON{Id: d.id, Name: d.name, Host: d.host})
}

func (d *Device) UnmarshalJSON(data []byte) error {
	var dto deviceJSON
	if err := json.Unmarshal(data, &dto); err != nil {
		return err
	}
	d.id = dto.Id
	d.name = dto.Name
	d.host = dto.Host
	return nil
}

func (d *Device) Refresh(ctx context.Context, via types.Channel) (bool, error) {
	// Gen1 devices cannot be refreshed via RPC
	if IsGen1Device(d.Id()) {
		d.log.V(1).Info("Skipping refresh for Gen1 device", "device_id", d.Id())
		return false, nil
	}

	// BLU devices (Generation 0) cannot be refreshed via RPC - they are updated via MQTT events only
	if IsBluDevice(d.Id()) {
		d.log.V(1).Info("Skipping refresh for BLU device (updated via MQTT events)", "device_id", d.Id())
		return false, nil
	}

	if !d.IsMqttReady() && d.Id() != "" {
		err := d.initMqtt(ctx)
		if err != nil {
			return false, fmt.Errorf("unable to init MQTT (%w)", err)
		}
	}

	// Resolve the channel after MQTT initialization - this ensures MQTT-only devices
	// (no known host) use MQTT instead of the discard caller for ChannelDefault
	via = d.Channel(ctx, via)

	if d.Id() == "" {
		err := d.initDeviceInfo(ctx, via)
		if err != nil {
			return d.IsModified(), fmt.Errorf("unable to init device (%w) using HTTP", err)
		}
	}
	if d.Mac() == nil {
		err := d.initDeviceInfo(ctx, via)
		if err != nil {
			return d.IsModified(), fmt.Errorf("unable to init device (%w)", err)
		}
	}

	// Fetch device info to store in database
	info, err := shelly.GetDeviceInfo(ctx, d, via)
	if err != nil {
		d.log.Error(err, "Unable to get device info")
		// Continue anyway - info is optional
	} else {
		d.info = info
	}

	crs, err := shelly.DoGetComponents(ctx, d, &shelly.ComponentsRequest{
		Include: []string{"config", "status"},
	})
	if err != nil {
		d.log.Error(err, "Unable to get device's components configuration")
		return false, err
	}

	d.config = &crs.Config
	if d.config.System == nil {
		// Some devices do not report "sys" in their components:  need to get it explicitelly
		d.config.System, err = system.GetConfig(ctx, via, d)
		if err != nil {
			d.log.Error(err, "Unable to get device's system configuration")
			return false, err
		}
	}
	d.UpdateName(d.config.System.Device.Name)

	d.status = &crs.Status
	if d.status.Wifi != nil {
		d.UpdateHost(d.status.Wifi.IP)
	}
	// Ethernet takes precedence over Wifi, when defined
	if d.status.Ethernet != nil {
		d.UpdateHost(d.status.Ethernet.IP)
	}

	d.modified = true
	return true, nil
}

func (d *Device) Manufacturer() string {
	return "Shelly"
}

func (d *Device) Id() string {
	if d.id == "" || d.id == "<nil>" {
		return ""
	}
	return d.id
}

func (d *Device) UpdateId(id string) {

	if id == "" || id == "<nil>" || !deviceIdRe.MatchString(id) {
		panic("invalid device id: " + id)
	}
	if d.id == id {
		return
	}
	d.id = id
	d.modified = true
}

func (d *Device) Host() string {

	if d.host == nil {
		return ""
	}
	return d.host.String()
}

func (d *Device) UpdateHost(host string) {
	if host == "" || host == "<nil>" {
		return
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return
	}
	if !ip.Equal(d.host) {
		d.modified = true
		d.host = ip
	}
}

func (d *Device) ClearHost() {
	if d.host != nil {
		d.modified = true
		d.host = nil
		d.log.Info("Cleared device host (HTTP channel will become not ready)", "device_id", d.Id())
	}
}

func (d *Device) Ip() net.IP {
	if d.status != nil {
		if d.status.Ethernet != nil {
			d.host = net.ParseIP(d.status.Ethernet.IP)
		} else if d.status.Wifi != nil {
			d.host = net.ParseIP(d.status.Wifi.IP)
		}
	}
	return d.host
}

func (d *Device) Name() string {
	if d.name == "" || d.name == "<nil>" {
		return ""
	}
	return d.name
}

func (d *Device) UpdateName(name string) {
	if name == "" || name == "<nil>" || name == d.name {
		return
	}
	d.name = name
	d.modified = true
}

func (d *Device) Mac() net.HardwareAddr {
	return d.macAddress
}

func (d *Device) UpdateMac(mac string) {
	if mac == d.macAddress.String() {
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
		d.macAddress = addr
	}
	d.modified = true
}

func (d *Device) Info() *shelly.DeviceInfo {
	return d.info
}

func (d *Device) Config() *shelly.Config {
	return d.config
}

func (d *Device) Status() *shelly.Status {
	return d.status
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
	return d.Id() != "" && d.mqtt.IsReady()
}

func (d *Device) IsHttpReady() bool {
	return d.Host() != ""

	// FIXME: check that the IP is on the same network as the Router
	// var ip net.IP
	// if d.status != nil {
	// 	if d.status.Ethernet != nil {
	// 		ip = net.ParseIP(d.status.Ethernet.IP)
	// 	} else if d.status.Wifi != nil {
	// 		ip = net.ParseIP(d.status.Wifi.IP)
	// 	}
	// } else {
	// 	ip = d.Host_
	// }

	// if ip == nil {
	// 	d.log.Info("Device has no IP address", "device", d)
	// 	return false
	// }
	// d.UpdateHost(ip.String())
	// return mynet.IsSameNetwork(d.log.V(1), ip) == nil
}

func (d *Device) StartDialog(ctx context.Context) uint32 {
	// Lock the MQTT channel to serialize request-response cycles
	d.mqtt.Lock()

	d.dialogId++
	d.dialogs.Store(d.dialogId, true)
	return d.dialogId
}

func (d *Device) StopDialog(ctx context.Context, id uint32) {
	d.dialogs.Delete(id)
	// Unlock the MQTT channel after the response is received
	d.mqtt.Unlock()
}

func (d *Device) Channel(ctx context.Context, via types.Channel) types.Channel {
	switch via {
	case types.ChannelDefault:
		if d.IsMqttReady() {
			return types.ChannelMqtt
		}
		if d.IsHttpReady() || d.resolveHost(ctx) {
			return types.ChannelHttp
		}
	case types.ChannelMqtt:
		if d.IsMqttReady() {
			return types.ChannelMqtt
		}
	case types.ChannelHttp:
		if d.IsHttpReady() || d.resolveHost(ctx) {
			return types.ChannelHttp
		}
	}
	// Auto discarded
	return types.ChannelDefault
}

// resolveHost asks the injected types.HostResolver (if any) for the
// device's current IP, keyed by MAC first and then by device ID (mDNS
// hostname). On success it updates Host_ and returns true.
func (d *Device) resolveHost(ctx context.Context) bool {
	ip, ok := types.ResolveHost(ctx, d.Mac(), d.Id())
	if !ok {
		return false
	}
	d.UpdateHost(ip.String())
	return true
}

var hostRe = regexp.MustCompile("^(?P<model>[a-zA-Z0-9]+)-(?P<serial>[A-Z0-9]+).local.$")

var generationRe = regexp.MustCompile("^gen=(?P<generation>[0-9]+)$")

var applicationRe = regexp.MustCompile("^app=(?P<application>[a-zA-Z0-9]+)$")

var versionRe = regexp.MustCompile("^ver=(?P<version>[.0-9]+)$")

var deviceIdRe = regexp.MustCompile("^shelly[a-zA-Z0-9]+-[a-f0-9]{12}$")

// Gen1 device ID prefixes that identify Gen1 devices
var gen1Prefixes = []string{
	"shellyht-",     // Shelly H&T (Humidity & Temperature)
	"shellyflood-",  // Shelly Flood
	"shelly1-",      // Shelly 1
	"shelly1pm-",    // Shelly 1PM
	"shelly25-",     // Shelly 2.5
	"shellyplug-",   // Shelly Plug
	"shellydimmer-", // Shelly Dimmer
	"shellyrgbw2-",  // Shelly RGBW2
	"shellybulb-",   // Shelly Bulb
	"shellydw-",     // Shelly Door/Window
	"shellyem-",     // Shelly EM
	"shelly3em-",    // Shelly 3EM
	"shellyuni-",    // Shelly UNI
}

// IsGen1Device returns true if the device ID indicates a Gen1 device
// Gen1 devices are identified by their ID prefix (e.g., "shellyht-", "shellyflood-")
func IsGen1Device(deviceId string) bool {
	for _, prefix := range gen1Prefixes {
		if strings.HasPrefix(deviceId, prefix) {
			return true
		}
	}
	return false
}

// BLU device ID prefixes that identify BLU devices.
// Must stay in sync with deviceIDFromCapabilities() in internal/myhome/shelly/blu/listener.go.
var bluPrefixes = []string{
	"shellyblu-",            // generic BLU fallback
	"shellybluht3-",         // BLU H&T v3
	"shellybludoorwindow2-", // BLU door/window v2
	"shellyblumotion1-",     // BLU motion v1
	"shellyblubutton1-",     // BLU button v1
	"sbht-",                 // alternate H&T naming
}

func IsBluDevice(deviceId string) bool {
	for _, prefix := range bluPrefixes {
		if strings.HasPrefix(deviceId, prefix) {
			return true
		}
	}
	return false
}

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

	// Per-device rate limiting with queuing
	rl := ratelimit.GetLimiter()
	if rl != nil {
		if err := rl.Wait(ctx, d.Id()); err != nil {
			return nil, err
		}
	}

	result, err := GetRegistrar().CallE(ctx, d, via, mh, params)

	// Mark command completion for rate limiting (interval measured from response to next request)
	if rl != nil {
		rl.Done(d.Id())
	}

	return result, err
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
	if d.mqtt == nil {
		return nil
	}
	return d.mqtt.To
}

func (d *Device) From() <-chan []byte {
	if d.mqtt == nil {
		return nil
	}
	return d.mqtt.From
}

func (d *Device) ReplyTo() string {
	if d.mqtt == nil {
		return ""
	}
	return d.mqtt.ReplyTo
}

func NewDeviceFromIp(ctx context.Context, log logr.Logger, ip net.IP) (*Device, error) {
	d := &Device{
		host: ip,
		log:  log,
	}
	err := d.init(ctx)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// MacFromShellyID extracts the MAC address embedded in a Shelly device ID of
// the form "<model>-<12hexchars>" (e.g. "shelly1minig3-54320464e730").
// Returns nil when the ID does not follow that pattern.
func MacFromShellyID(id string) net.HardwareAddr {
	i := strings.LastIndex(id, "-")
	if i < 0 {
		return nil
	}
	h := id[i+1:]
	if len(h) != 12 {
		return nil
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return nil
		}
	}
	mac, err := net.ParseMAC(fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		h[0:2], h[2:4], h[4:6], h[6:8], h[8:10], h[10:12]))
	if err != nil {
		return nil
	}
	return mac
}

func NewDeviceFromMqttId(ctx context.Context, log logr.Logger, id string) (*Device, error) {
	if id == "" || id == "<nil>" {
		return nil, fmt.Errorf("invalid device id: %s", id)
	}
	d := &Device{
		log: log,
	}
	d.UpdateId(id)
	if mac := MacFromShellyID(id); mac != nil {
		d.UpdateMac(mac.String())
	}
	return d, nil
}

func NewDeviceFromSummary(ctx context.Context, log logr.Logger, summary Summary) (*Device, error) {
	if summary.Id() == "" {
		return nil, fmt.Errorf("device summary has empty ID (name=%q, host=%q)", summary.Name(), summary.Host())
	}
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

// Init initializes the device, setting up HTTP and MQTT channels as needed.
// This is the exported version of init() for use by external packages.
func (d *Device) Init(ctx context.Context) error {
	return d.init(ctx)
}

func (d *Device) initHttp(ctx context.Context) error {
	if d == nil {
		panic("device is nil")
	}
	if !d.IsHttpReady() {
		return fmt.Errorf("device host is empty: no channel to communicate with HTTP")
	}
	// Best-effort: device info can be fetched later; the HTTP channel itself is ready.
	if err := d.initDeviceInfo(ctx, types.ChannelHttp); err != nil {
		d.log.V(1).Info("Unable to init device info (will retry later)", "via", types.ChannelHttp, "error", err)
	}
	return nil
}

func (d *Device) initMqtt(ctx context.Context) error {
	if d == nil {
		panic("device is nil")
	}

	if d.Id() == "" {
		panic("device id is empty: no channel to communicate")
	}

	var err error
	d.mqtt, err = d.mqtt.Init(ctx, d.Id())
	if err != nil {
		d.log.Error(err, "Unable to init MQTT channels", "device_id", d.id)
		return err
	}

	d.log.V(1).Info("MQTT channels ready", "device_id", d.Id())
	// Best-effort: device info can be fetched later; the MQTT channel itself is ready.
	if err := d.initDeviceInfo(ctx, types.ChannelMqtt); err != nil {
		d.log.V(1).Info("Unable to init device info (will retry later)", "via", types.ChannelMqtt, "error", err)
	}
	return nil
}

func (d *Device) initDeviceInfo(ctx context.Context, via types.Channel) error {
	if d == nil {
		panic("device is nil")
	}
	if IsBluDevice(d.Id()) {
		return nil
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

// // refreshScripts fetches the list of scripts from the device and stores them in config
// func (d *Device) refreshScripts(ctx context.Context, via types.Channel) error {
// 	out, err := d.CallE(ctx, via, "Script.List", nil)
// 	if err != nil {
// 		return err
// 	}

// 	// Use the existing script.ListResponse type
// 	resp, ok := out.(*script.ListResponse)
// 	if !ok {
// 		d.log.V(1).Info("Script.List response type mismatch", "type", fmt.Sprintf("%T", out))
// 		return nil
// 	}

// 	if d.config == nil {
// 		d.config = &shelly.Config{}
// 	}

// 	d.config.Scripts = make([]shelly.ScriptInfo, len(resp.Scripts))
// 	for i, s := range resp.Scripts {
// 		d.config.Scripts[i] = shelly.ScriptInfo{
// 			Id:      s.Id,
// 			Name:    s.Name,
// 			Running: s.Running,
// 			// Note: script.Status doesn't have Enable field, it's in Configuration
// 		}
// 	}
// 	d.modified = true
// 	d.log.V(1).Info("Refreshed scripts", "count", len(d.config.Scripts))
// 	return nil
// }

type Do func(context.Context, logr.Logger, types.Channel, Summary, []string) (any, error)

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
	Device Summary
	Result any
	Error  error
}

func Foreach(ctx context.Context, log logr.Logger, deviceList []Summary, via types.Channel, do Do, args []string) (any, error) {
	log.Info("Running", "func_type", reflect.TypeOf(do), "args", args, "nb_devices", len(deviceList))

	// Create channels for results
	results := make(chan DeviceResult, len(deviceList))
	var wg sync.WaitGroup

	// Process each device in parallel
	for _, dev := range deviceList {
		wg.Add(1)
		go func(devSummary Summary) {
			defer wg.Done()

			// Skip devices whose ID is not yet known (partially discovered)
			if devSummary.Id() == "" {
				log.V(1).Info("Skipping device with no ID yet", "name", devSummary.Name(), "host", devSummary.Host())
				results <- DeviceResult{Device: devSummary, Error: nil}
				return
			}

			// Skip Gen1 devices - they cannot receive commands or run scripts
			if IsGen1Device(devSummary.Id()) {
				log.V(1).Info("Skipping Gen1 device (no command/script support)", "device_id", devSummary.Id())
				results <- DeviceResult{Device: devSummary, Error: nil}
				return
			}

			// Skip BLU devices (Generation 0) - they cannot receive commands or run scripts
			if IsBluDevice(devSummary.Id()) {
				log.V(1).Info("Skipping BLU device (no command/script support)", "device_id", devSummary.Id())
				results <- DeviceResult{Device: devSummary, Error: nil}
				return
			}

			// Create device from summary
			device, err := NewDeviceFromSummary(ctx, log, devSummary)
			if err != nil {
				log.Error(err, "Unable to create device from summary", "device", devSummary)
				results <- DeviceResult{Device: devSummary, Error: err}
				return
			}

			// Initialize device
			err = device.init(ctx)
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
