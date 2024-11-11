package http

import (
	"devices/shelly/gen1"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"

	"github.com/go-logr/logr"
	"github.com/gorilla/schema"
)

// User-Agent: [Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)]
var uaRe = regexp.MustCompile(`^\[Shelly/(?P<fw_date>[0-9-]+)/(?P<fw_id>[a-z0-9-.]+) \((?P<model>[A-Z0-9-]+)\)\]$`)

func MyHome(log logr.Logger, g1c chan gen1.Device) {
	var decoder = schema.NewDecoder()

	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		for k, v := range req.Header {
			log.Info("header: %s: %s", k, v)
		}

		var d gen1.Device
		ua := req.Header["User-Agent"][0]
		if uaRe.Match([]byte(ua)) {
			d.FirmwareDate = uaRe.ReplaceAllString(ua, "${fw_date}")
			d.FirmwareId = uaRe.ReplaceAllString(ua, "${fw_id}")
			d.Model = uaRe.ReplaceAllString(ua, "${model}")
		}

		ip, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			log.Info("http.HandleFunc: not <ip>:<port>", "userip", req.RemoteAddr)
		}
		d.Ip = net.ParseIP(ip)
		if d.Ip == nil {
			log.Info("http.HandleFunc: not <ip>:<port>", "userip", req.RemoteAddr)
			return
		}

		log.Info("http.HandleFunc", "url", req.URL)
		m, _ := url.ParseQuery(req.URL.RawQuery)
		log.Info("http.HandleFunc", "query", m)

		var ht gen1.HTSensor
		err = decoder.Decode(&ht, m)
		if err == nil {
			d.HTSensor = &ht
		}

		var fl gen1.Flood
		err = decoder.Decode(&fl, m)
		if err == nil {
			d.Flood = &fl
		}

		// var t any
		// err := json.NewDecoder(r.Body).Decode(&t)
		// if err != nil {
		// 	http.Error(w, err.Error(), http.StatusBadRequest)
		// 	return
		// }
		// tc <- req.Body

		log.Info("http.HandleFunc", "gen1_device", d)
		g1c <- d

		jd, _ := json.Marshal(d)
		log.Info("http.HandleFunc", "gen1_json", string(jd))

		_, _ = w.Write([]byte("")) // 200 OK
	})
	log.Info("Now listening on port 8888.")
	http.ListenAndServe(":8888", nil)
}
