package gen1

import (
	"context"
	"encoding/json"
	"fmt"
	mqttclient "myhome/mqtt"
	"net"
	"net/http"
	"net/url"
	shellymqtt "pkg/shelly/mqtt"
	"pkg/shelly/temperature"
	"regexp"

	"github.com/go-logr/logr"
	"github.com/gorilla/schema"
)

type Empty struct{}

// User-Agent: [Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)]
var uaRe = regexp.MustCompile(`^\[Shelly/(?P<fw_date>[0-9-]+)/(?P<fw_id>[a-z0-9-.]+) \((?P<model>[A-Z0-9-]+)\)\]$`)

type httpProxy struct {
	ctx      context.Context
	log      logr.Logger
	mc       *mqttclient.Client
	dialogId uint32
	decoder  *schema.Decoder
}

func Proxy(ctx context.Context, log logr.Logger, port int, mc *mqttclient.Client) {
	hp := httpProxy{
		ctx:      ctx,
		log:      log,
		mc:       mc,
		dialogId: 0, // independent counter (we do not expect replies)
		decoder:  schema.NewDecoder(),
	}
	go http.ListenAndServe(fmt.Sprintf(":%d", port), &hp)
}

func (hp *httpProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	for k, v := range req.Header {
		hp.log.Info("Inbound", k, v)
	}

	var d Device
	ua := req.Header["User-Agent"][0]
	if uaRe.Match([]byte(ua)) {
		d.FirmwareDate = uaRe.ReplaceAllString(ua, "${fw_date}")
		d.FirmwareId = uaRe.ReplaceAllString(ua, "${fw_id}")
		d.Model = uaRe.ReplaceAllString(ua, "${model}")
	} else {
		hp.log.Error(fmt.Errorf("unknown User-Agent: %s", ua), "http.HandleFunc: unknown User-Agent", "remote_addr", req.RemoteAddr)
		return
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

	hp.log.Info("http.HandleFunc", "url", req.URL)
	m, _ := url.ParseQuery(req.URL.RawQuery)
	hp.log.Info("http.HandleFunc", "query", m)

	var ht HTSensor
	err = hp.decoder.Decode(&ht, m)
	if err == nil {
		d.HTSensor = &ht
	}

	var fl Flood
	err = hp.decoder.Decode(&fl, m)
	if err == nil {
		d.Flood = &fl
	}

	// Trying to rebuild <https://shelly-api-docs.shelly.cloud/gen1/#shelly-h-amp-t-mqtt>
	topic, msg, err := hp.formatAsGen2(d)
	if err != nil {
		hp.log.Error(err, "http.HandleFunc: unable to format message", "message", d)
		return
	}
	hp.mc.Publish(topic, msg)

	hp.dialogId++
	_, _ = w.Write([]byte("")) // 200 OK
}

// <https://shelly-api-docs.shelly.cloud/gen2/General/RPCChannels#mqtt>
func (hp *httpProxy) formatAsGen2(device Device) (topic string, msg []byte, err error) {
	topic = fmt.Sprintf("%s/events/rpc", device.Id)
	var tC float32
	if device.HTSensor != nil {
		tC = device.HTSensor.Temperature
	}
	if device.Flood != nil {
		tC = device.Flood.Temperature
	}
	t := temperature.Status{
		Id:         0,
		Celsius:    tC,
		Fahrenheit: (tC * 1.8) + 32.0,
	}
	req := &shellymqtt.Request{
		Dialog: shellymqtt.Dialog{
			Id:  hp.dialogId,
			Src: fmt.Sprintf("%s_%s", hp.mc.Id(), device.Id),
		},
		Method: "NotifyStatus",
		Params: t,
	}
	msg, err = json.Marshal(req)
	if err != nil {
		return "", nil, err
	}
	return
}
