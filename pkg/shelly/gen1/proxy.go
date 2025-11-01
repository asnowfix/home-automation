package gen1

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"pkg/shelly/mqtt"
	"regexp"
	"sync"

	"github.com/go-logr/logr"
	"github.com/gorilla/schema"
)

type Empty struct{}

// User-Agent: Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)
var uaRe = regexp.MustCompile(`^\[?Shelly/(?P<fw_date>[0-9-]+)/(?P<fw_id>[a-z0-9.-]+) \((?P<model>[A-Z0-9-]+)\)\]?$`)

type http2MqttProxy struct {
	ctx     context.Context
	log     logr.Logger
	mc      mqtt.Client
	decoder *schema.Decoder
	// Cache for latest sensor values: key = "deviceId:sensor" (e.g., "shellyht-208500:temperature")
	cache sync.Map
	// Track subscribed devices to avoid duplicate subscriptions
	subscribedDevices sync.Map
}

func StartHttp2MqttProxy(ctx context.Context, port int, mc mqtt.Client) {
	hp := http2MqttProxy{
		ctx:     ctx,
		log:     logr.FromContextOrDiscard(ctx).WithName("Http2MqttProxy"),
		mc:      mc,
		decoder: schema.NewDecoder(),
	}
	go http.ListenAndServe(fmt.Sprintf(":%d", port), &hp)
}

func (hp *http2MqttProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer w.Write([]byte("")) // 200 OK

	for k, v := range req.Header {
		hp.log.Info("Inbound Header", k, v)
	}

	var d Device
	ua := req.Header["User-Agent"][0]
	if uaRe.Match([]byte(ua)) {
		d.FirmwareDate = uaRe.ReplaceAllString(ua, "${fw_date}")
		d.FirmwareId = uaRe.ReplaceAllString(ua, "${fw_id}")
		d.Model = uaRe.ReplaceAllString(ua, "${model}")
	} else {
		// User-Agent: Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)
		d.FirmwareDate = "2000-01-01"
		d.FirmwareId = "v0.0.0"
		d.Model = "UNKNONW"
		hp.log.Error(fmt.Errorf("unknown User-Agent: %s", ua), "http.HandleFunc: unknown User-Agent", "remote_addr", req.RemoteAddr)
	}

	ip, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		hp.log.Error(err, "http.HandleFunc: not formatted as <ip>:<port>", "remote_addr", req.RemoteAddr)
		return
	}
	d.Ip = net.ParseIP(ip)
	if d.Ip == nil {
		hp.log.Error(err, "http.HandleFunc: not an IP in <ip>:<port>", "remote_addr", req.RemoteAddr)
		return
	}

	hp.log.Info("Gen1 notification", "device", d)

	hp.log.Info("http.HandleFunc", "url", req.URL)
	m, _ := url.ParseQuery(req.URL.RawQuery)
	hp.log.Info("http.HandleFunc", "query", m)

	// Decode query parameters into Device struct - schema decoder will populate
	// the appropriate fields based on what's present in the query
	err = hp.decoder.Decode(&d, m)
	if err != nil {
		hp.log.Error(err, "http.HandleFunc: failed to decode query parameters", "query", m)
		return
	}

	// Emit Gen1 MQTT format as defined in <https://shelly-api-docs.shelly.cloud/gen1/#shelly-h-amp-t-mqtt>
	err = hp.publishAsGen1MQTT(d)
	if err != nil {
		hp.log.Error(err, "http.HandleFunc: unable to publish MQTT message as Gen1", "device", d)
		return
	}
}

// publishAsGen1MQTT publishes messages in Gen1 MQTT format
// Format: shellies/<device-id>/sensor/<sensor-type> with JSON payload
// See: https://shelly-api-docs.shelly.cloud/gen1/#shelly-h-amp-t-mqtt
func (hp *http2MqttProxy) publishAsGen1MQTT(device Device) error {
	// Publish temperature (common to both H&T and Flood sensors)
	tempTopic := fmt.Sprintf("shellies/%s/sensor/temperature", device.Id)
	tempMsg, err := json.Marshal(device.Temperature)
	if err != nil {
		return fmt.Errorf("failed to marshal temperature: %w", err)
	}
	hp.mc.Publish(hp.ctx, tempTopic, tempMsg)
	hp.log.Info("Published Gen1 MQTT", "topic", tempTopic, "value", device.Temperature)
	
	// Cache temperature value for request/response
	hp.cache.Store(fmt.Sprintf("%s:temperature", device.Id), device.Temperature)
	
	// Subscribe to temperature requests for this device (if not already subscribed)
	hp.subscribeToRequests(device.Id, "temperature")

	if device.IsHTSensor() {
		// Publish humidity (H&T sensor only)
		humTopic := fmt.Sprintf("shellies/%s/sensor/humidity", device.Id)
		humMsg, err := json.Marshal(*device.Humidity)
		if err != nil {
			return fmt.Errorf("failed to marshal humidity: %w", err)
		}
		hp.mc.Publish(hp.ctx, humTopic, humMsg)
		hp.log.Info("Published Gen1 MQTT", "topic", humTopic, "value", *device.Humidity)
		
		// Cache humidity value for request/response
		hp.cache.Store(fmt.Sprintf("%s:humidity", device.Id), *device.Humidity)
		
		// Subscribe to humidity requests for this device (if not already subscribed)
		hp.subscribeToRequests(device.Id, "humidity")
	}

	if device.IsFloodSensor() {
		// Publish flood status
		floodTopic := fmt.Sprintf("shellies/%s/sensor/flood", device.Id)
		floodMsg, err := json.Marshal(*device.Flood)
		if err != nil {
			return fmt.Errorf("failed to marshal flood: %w", err)
		}
		hp.mc.Publish(hp.ctx, floodTopic, floodMsg)
		hp.log.Info("Published Gen1 MQTT", "topic", floodTopic, "value", *device.Flood)

		// Publish battery voltage
		batTopic := fmt.Sprintf("shellies/%s/sensor/battery", device.Id)
		batMsg, err := json.Marshal(*device.BatteryVoltage)
		if err != nil {
			return fmt.Errorf("failed to marshal battery: %w", err)
		}
		hp.mc.Publish(hp.ctx, batTopic, batMsg)
		hp.log.Info("Published Gen1 MQTT", "topic", batTopic, "value", *device.BatteryVoltage)
	}

	return nil
}

// subscribeToRequests subscribes to request topic for a specific device and sensor type
// This is called when a device first publishes data to cache the subscription
func (hp *http2MqttProxy) subscribeToRequests(deviceId string, sensorType string) {
	// Check if already subscribed
	subscriptionKey := fmt.Sprintf("%s:%s", deviceId, sensorType)
	if _, loaded := hp.subscribedDevices.LoadOrStore(subscriptionKey, true); loaded {
		// Already subscribed
		return
	}
	
	requestTopic := fmt.Sprintf("shellies/%s/sensor/%s/request", deviceId, sensorType)
	reqChan, err := hp.mc.Subscriber(hp.ctx, requestTopic, 0)
	if err != nil {
		hp.log.Error(err, "Failed to subscribe to request topic", "topic", requestTopic)
		return
	}
	
	hp.log.Info("Subscribed to request topic", "topic", requestTopic)
	
	// Start goroutine to handle requests for this device/sensor
	go func() {
		for {
			select {
			case <-hp.ctx.Done():
				return
			case <-reqChan:
				// Request received - respond with cached value
				cacheKey := fmt.Sprintf("%s:%s", deviceId, sensorType)
				if value, ok := hp.cache.Load(cacheKey); ok {
					responseTopic := fmt.Sprintf("shellies/%s/sensor/%s", deviceId, sensorType)
					responseMsg, _ := json.Marshal(value)
					hp.mc.Publish(hp.ctx, responseTopic, responseMsg)
					hp.log.Info("Responded to request", "device", deviceId, "sensor", sensorType, "value", value)
				} else {
					hp.log.Info("No cached value for request", "device", deviceId, "sensor", sensorType)
				}
			}
		}
	}()
}
