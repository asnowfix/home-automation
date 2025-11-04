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
	// Publish on device info topic (not used by real Gen1 devices)
	infoTopic := fmt.Sprintf("shellies/%s/info", device.Id)
	infoMsg, err := json.Marshal(device)
	if err != nil {
		return fmt.Errorf("failed to marshal device info: %w", err)
	}
	hp.mc.Publish(hp.ctx, infoTopic, infoMsg)
	hp.log.Info("Published Gen1 MQTT", "topic", infoTopic, "value", device)

	// Publish temperature (common to both H&T and Flood sensors)
	if device.Temperature != nil {
		tempTopic := fmt.Sprintf("shellies/%s/sensor/temperature", device.Id)
		tempMsg, err := json.Marshal(device.Temperature)
		if err != nil {
			return fmt.Errorf("failed to marshal temperature: %w", err)
		}
		hp.mc.Publish(hp.ctx, tempTopic, tempMsg)
		hp.log.Info("Published Gen1 MQTT", "topic", tempTopic, "value", device.Temperature)
	}

	if device.Humidity != nil {
		// Publish humidity (H&T sensor only)
		humTopic := fmt.Sprintf("shellies/%s/sensor/humidity", device.Id)
		humMsg, err := json.Marshal(device.Humidity)
		if err != nil {
			return fmt.Errorf("failed to marshal humidity: %w", err)
		}
		hp.mc.Publish(hp.ctx, humTopic, humMsg)
		hp.log.Info("Published Gen1 MQTT", "topic", humTopic, "value", device.Humidity)
	}
	if device.Flood != nil {
		// Publish flood status
		floodTopic := fmt.Sprintf("shellies/%s/sensor/flood", device.Id)
		floodMsg, err := json.Marshal(device.Flood)
		if err != nil {
			return fmt.Errorf("failed to marshal flood: %w", err)
		}
		hp.mc.Publish(hp.ctx, floodTopic, floodMsg)
		hp.log.Info("Published Gen1 MQTT", "topic", floodTopic, "value", device.Flood)

		// Publish battery voltage
		batTopic := fmt.Sprintf("shellies/%s/sensor/battery", device.Id)
		batMsg, err := json.Marshal(device.BatV)
		if err != nil {
			return fmt.Errorf("failed to marshal battery: %w", err)
		}
		hp.mc.Publish(hp.ctx, batTopic, batMsg)
		hp.log.Info("Published Gen1 MQTT", "topic", batTopic, "value", device.BatV)
	}

	return nil
}
