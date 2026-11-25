package shelly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
	_ "github.com/asnowfix/home-automation/internal/myhome/net"
	"net"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/ratelimit"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/system"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"reflect"
	"regexp"
	"strings"
	"sync"

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
	if mc == nil {
		return nil, fmt.Errorf("MQTT client not initialized (ctx carries no pkg/shelly/mqtt.Client — composition root never wrapped it)")
	}
	replyTo := fmt.Sprintf("%s_%s", mc.Id(), deviceId)

	// The registry entry outlives this caller: every later Device for the same
	// id reuses it. The MQTT client ties a subscription and a publisher to the
	// ctx they are created with (the subscriber queue is closed and the publisher
	// stops when it ends), so they must not inherit this caller's cancellation or
	// deadline. Otherwise a short-lived first caller, such as the reachability
	// probe's timeout ctx, leaves a closed reply channel in the registry, and every
	// later call on that device reads an empty payload (#905). WithoutCancel keeps
	// ctx's values, including the MQTT client.
	ctx = context.WithoutCancel(ctx)

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

type Device struct {
	Id_         string              `json:"id"`
	MacAddress_ net.HardwareAddr    `json:"-"`
	Name_       string              `json:"name"`
	Host_       net.IP              `json:"host"`
	info        *shelly.DeviceInfo  `json:"-"`
	config      *shelly.Config      `json:"-"`
	status      *shelly.Status      `json:"-"` // immutable once published: replaced, never mutated (see status.go)
	statusMu    sync.RWMutex        `json:"-"` // guards the status pointer
	mqtt        *DeviceMqttChannels `json:"-"` // MQTT channels (shared via registry, includes mutex for serialization)
	dialogId    uint32              `json:"-"`
	dialogs     sync.Map            `json:"-"` // map[uint32]bool
	log         logr.Logger         `json:"-"`
	modified    bool                `json:"-"`
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
			return false, fmt.Errorf("unable to init MQTT (%v)", err)
		}
	}

	// Resolve the channel after MQTT initialization - this ensures MQTT-only devices
	// (no known host) use MQTT instead of the discard caller for ChannelDefault
	via = d.Channel(ctx, via)

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

	// // If device is not ready for HTTP, try to find IP
	// if !d.IsHttpReady() {
	// 	if d.status == nil {
	// 		d.status = &shelly.Status{}
	// 	}
	// 	ws, err := wifi.DoGetStatus(ctx, via, d)
	// 	d.log.Info("Wifi status", "device", d.Id(), "status", ws, "error", err)
	// 	if err == nil && ws.IP != "" {
	// 		d.status.Wifi = ws
	// 		d.Host_ = net.ParseIP(ws.IP)
	// 		d.UpdateHost(ws.IP)
	// 	}
	// 	es, err := ethernet.DoGetStatus(ctx, via, d)
	// 	d.log.Info("Ethernet status", "device", d.Id(), "status", es, "error", err)
	// 	if err == nil && es.IP != "" {
	// 		d.status.Ethernet = es
	// 		d.UpdateHost(es.IP)
	// 	}
	// 	d.log.Info("Will use IP", "device", d.Id(), "ip", d.Host())
	// }

	// // Always fetch system config to get current device name
	// config, err := system.GetConfig(ctx, via, d)
	// if err != nil {
	// 	return d.IsModified(), fmt.Errorf("unable to system.GetDeviceConfig (%v)", err)
	// }
	// if d.config == nil {
	// 	d.config = &shelly.Config{}
	// }
	// d.config.System = config
	// if config.Device.Name != "" && config.Device.Name != d.Name() {
	// 	d.UpdateName(config.Device.Name)
	// }

	// // Fetch scripts list and store in config
	// if err := d.refreshScripts(ctx, via); err != nil {
	// 	d.log.V(1).Info("Failed to refresh scripts (continuing)", "error", err)
	// }

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

	status := &crs.Status
	d.SetStatus(status)
	if status.Wifi != nil {
		d.UpdateHost(status.Wifi.IP)
	}
	// Ethernet takes precedence over Wifi, when defined
	if status.Ethernet != nil {
		d.UpdateHost(status.Ethernet.IP)
	}

	d.modified = true
	return true, nil
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

// checkDeviceId reports whether id is a device id this package can represent.
//
// Ids are deliberately NOT normalised (e.g. lower-cased): Gen1 devices publish
// upper-case ids on MQTT (shellies/<ID>/...), and the id is also the database key,
// so rewriting it here would make it diverge from both. An id that does not match
// is rejected so that callers can skip the device instead of crashing.
func checkDeviceId(id string) error {
	if id == "" || id == "<nil>" || !deviceIdRe.MatchString(id) {
		return fmt.Errorf("invalid device id: %q", id)
	}
	return nil
}

// UpdateId sets the device id. An invalid id is logged and ignored (it used to panic,
// crash-looping the daemon, #800); constructors validate with checkDeviceId first and
// return the error. The method keeps its error-less signature because it is part of
// the types.Device interface, which has many implementations.
func (d *Device) UpdateId(id string) {
	if err := checkDeviceId(id); err != nil {
		d.log.Error(err, "Ignoring invalid device id")
		return
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

func (d *Device) ClearHost() {
	if d.Host_ != nil {
		d.modified = true
		d.Host_ = nil
		d.log.Info("Cleared device host (HTTP channel will become not ready)", "device_id", d.Id())
	}
}

func (d *Device) Ip() net.IP {
	if status := d.Status(); status != nil {
		if status.Ethernet != nil {
			d.Host_ = net.ParseIP(status.Ethernet.IP)
		} else if status.Wifi != nil {
			d.Host_ = net.ParseIP(status.Wifi.IP)
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

// Status returns the current status snapshot. The returned value must be
// treated as read-only: it may be shared with concurrent readers.
func (d *Device) Status() *shelly.Status {
	d.statusMu.RLock()
	defer d.statusMu.RUnlock()
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

var nameRe = regexp.MustCompile(fmt.Sprintf("^(?P<id>[a-zA-Z0-9]+).%s.local.$", MDNS_SHELLIES))

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
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
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

func NewDeviceFromMqttId(ctx context.Context, log logr.Logger, id string) (devices.Device, error) {
	if id == "" || id == "<nil>" {
		return nil, fmt.Errorf("invalid device id: %s", id)
	}
	if err := checkDeviceId(id); err != nil {
		return nil, err
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

func NewDeviceFromSummary(ctx context.Context, log logr.Logger, summary devices.Device) (devices.Device, error) {
	if summary.Id() == "" {
		return nil, fmt.Errorf("device summary has empty ID (name=%q, host=%q)", summary.Name(), summary.Host())
	}
	if err := checkDeviceId(summary.Id()); err != nil {
		return nil, fmt.Errorf("device summary (name=%q, host=%q): %w", summary.Name(), summary.Host(), err)
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
	d.initDeviceInfo(ctx, types.ChannelHttp)
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
		d.log.Error(err, "Unable to init MQTT channels", "device_id", d.Id_)
		return err
	}

	d.log.V(1).Info("MQTT channels ready", "device_id", d.Id())
	d.initDeviceInfo(ctx, types.ChannelMqtt)
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
		if err := checkDeviceId(info.Id); err != nil {
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
	// Unreachable is set (and Error is nil) when the device is registered but
	// did not answer the short reachability probe (see ForeachOptions).
	Unreachable error
}

// ErrNoReachableDevice is returned by Foreach when every device it was asked
// to process was unreachable, so that nothing at all was done. A single
// explicitly named device that is down must not look like a success.
var ErrNoReachableDevice = errors.New("no reachable device")

// ForeachOptions tunes ForeachWith. The zero value reproduces Foreach exactly.
type ForeachOptions struct {
	// ProbeTimeout, when > 0, makes ForeachWith probe each device with one
	// cheap RPC (Shelly.GetDeviceInfo) bounded by this duration, before
	// running the operation. A device whose probe times out is reported as
	// "unreachable" (skipped, listed apart from failures, no effect on the
	// exit code) instead of burning the full MQTT/HTTP timeout on every call
	// the operation would make. Any other probe outcome (an answer, even an
	// RPC error; or a non-timeout error) lets the device proceed: a device
	// that merely flapped but answers now is NOT skipped.
	//
	// "Unreachable" is decided by the probe alone, never by a registry flag:
	// the registry summary carries no online state, devices.Host.Online() is a
	// hardcoded true for mDNS-found devices, and a stored "offline" can be
	// stale right after a flap. Only an unanswered probe, now, counts.
	ProbeTimeout time.Duration

	// Probe replaces the default probe (test seam). It is called before the
	// device is initialised and must return an error satisfying IsUnreachable
	// to mark the device unreachable.
	Probe func(ctx context.Context, log logr.Logger, via types.Channel, summary devices.Device) error
}

// IsUnreachable reports whether err is the kind of failure that means "the
// device did not answer in time" (probe deadline, MQTT/HTTP timeout), as
// opposed to a device that answered with an error or a local failure.
func IsUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return strings.Contains(err.Error(), "timeout waiting for response")
}

// probeDevice sends one Shelly.GetDeviceInfo bounded by timeout. It returns nil
// when the device answered (even with an RPC error), an IsUnreachable error
// when it did not, and any other error for local failures.
func probeDevice(ctx context.Context, log logr.Logger, via types.Channel, summary devices.Device, timeout time.Duration) error {
	device, err := NewDeviceFromSummary(ctx, log, summary)
	if err != nil {
		return err
	}
	sd, ok := device.(*Device)
	if !ok {
		return fmt.Errorf("invalid device type %T", device)
	}
	// init may itself call the device (a summary without MAC), so it is bound
	// by the probe deadline too.
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := sd.init(pctx); err != nil {
		if IsUnreachable(err) && ctx.Err() == nil {
			return err
		}
		return nil // local init failure: not conclusive, the real run reports it
	}
	_, err = shelly.GetDeviceInfo(pctx, sd, via)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return err // the caller cancelled: not the device's fault
	}
	var rpcErr *types.RpcError
	if errors.As(err, &rpcErr) {
		return nil // it answered
	}
	return err
}

// Foreach runs do on every device, in parallel, with no reachability probe.
func Foreach(ctx context.Context, log logr.Logger, deviceList []devices.Device, via types.Channel, do Do, args []string) (any, error) {
	return ForeachWith(ctx, log, deviceList, via, do, args, ForeachOptions{})
}

// ForeachWith is Foreach with options. It returns an error only for real
// failures on reachable devices (or when no device at all was reachable);
// unreachable devices are listed in the summary but do not fail the run.
func ForeachWith(ctx context.Context, log logr.Logger, deviceList []devices.Device, via types.Channel, do Do, args []string, opts ForeachOptions) (any, error) {
	log.Info("Running", "func_type", reflect.TypeOf(do), "args", args, "nb_devices", len(deviceList))

	probe := opts.Probe
	if probe == nil && opts.ProbeTimeout > 0 {
		probe = func(ctx context.Context, log logr.Logger, via types.Channel, summary devices.Device) error {
			return probeDevice(ctx, log, via, summary, opts.ProbeTimeout)
		}
	}

	// Create channels for results
	results := make(chan DeviceResult, len(deviceList))
	var wg sync.WaitGroup

	// Process each device in parallel
	for _, dev := range deviceList {
		wg.Add(1)
		go func(devSummary devices.Device) {
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

			// Known but silent devices: skip quickly, do not fail.
			if probe != nil {
				if perr := probe(ctx, log, via, devSummary); perr != nil && IsUnreachable(perr) && ctx.Err() == nil {
					log.Info("Skipping unreachable device", "id", devSummary.Id(), "name", devSummary.Name(), "reason", perr.Error())
					results <- DeviceResult{Device: devSummary, Unreachable: perr}
					return
				}
				// Any other probe error is not conclusive: let the real
				// operation run and report its own failure.
			}

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
	var failed, unreachable []DeviceResult
	for result := range results {
		switch {
		case result.Unreachable != nil:
			unreachable = append(unreachable, result)
			continue // no result to report for a device that was not run
		case result.Error != nil:
			failed = append(failed, result)
		}
		out = append(out, result.Result)
	}
	// Goroutines finish in any order: sort so the summary is deterministic.
	byId := func(r []DeviceResult) {
		sort.Slice(r, func(a, b int) bool { return r[a].Device.Id() < r[b].Device.Id() })
	}
	byId(failed)
	byId(unreachable)

	succeeded := len(deviceList) - len(failed) - len(unreachable)
	writeForeachSummary(os.Stdout, succeeded, failed, unreachable)
	if len(failed) > 0 || len(unreachable) > 0 {
		log.Info("Operation completed with exceptions", "succeeded", succeeded, "failed", len(failed), "unreachable", len(unreachable), "total", len(deviceList))
	}

	if len(failed) > 0 {
		if len(failed) == len(deviceList) {
			return out, fmt.Errorf("all %d device(s) failed", len(deviceList))
		}
		return out, fmt.Errorf("%d of %d device(s) failed", len(failed), len(deviceList))
	}
	if len(unreachable) > 0 && len(unreachable) == len(deviceList) {
		return out, fmt.Errorf("%w: all %d device(s) unreachable", ErrNoReachableDevice, len(deviceList))
	}
	return out, nil
}

// writeForeachSummary prints the end-of-run summary: succeeded count, then
// failures (with reasons, #863), then unreachable devices, listed apart.
func writeForeachSummary(w io.Writer, succeeded int, failed, unreachable []DeviceResult) {
	if len(failed) == 0 && len(unreachable) == 0 {
		return
	}
	fmt.Fprintf(w, "\n")
	if succeeded > 0 {
		fmt.Fprintf(w, "✓ %d device(s) succeeded\n", succeeded)
	}
	if len(failed) > 0 {
		fmt.Fprintf(w, "✗ %d device(s) failed:\n", len(failed))
		for _, r := range failed {
			// The reason is part of the summary: without it a fleet run
			// (`script update '*'`) reports only names, and the cause of its
			// failures is lost (#863).
			fmt.Fprintf(w, "  - %s (%s): %v\n", r.Device.Name(), r.Device.Id(), r.Error)
		}
	}
	if len(unreachable) > 0 {
		fmt.Fprintf(w, "⚠ %d device(s) unreachable (skipped, not counted as failures):\n", len(unreachable))
		for _, r := range unreachable {
			fmt.Fprintf(w, "  - %s (%s)\n", r.Device.Name(), r.Device.Id())
		}
	}
}
