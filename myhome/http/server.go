package http

import (
	"devices/shelly/gen1"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"

	"github.com/gorilla/schema"
)

// User-Agent: [Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)]
var uaRe = regexp.MustCompile(`^\[Shelly/(?P<fw_date>[0-9-]+)/(?P<fw_id>[a-z0-9-.]+) \((?P<model>[A-Z0-9-]+)\)\]$`)

func MyHome(tc chan gen1.Device) {
	var decoder = schema.NewDecoder()

	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		log.Default().Printf("url: %s", req.URL)

		m, _ := url.ParseQuery(req.URL.RawQuery)
		log.Default().Printf("query: %v", m)

		var h gen1.Device
		err := decoder.Decode(&h, m)
		if err != nil {
			log.Default().Print(err)
			return
		}

		for k, v := range req.Header {
			log.Default().Printf("header: %s: %s", k, v)
		}

		ua := req.Header["User-Agent"][0]
		if uaRe.Match([]byte(ua)) {
			h.FirmwareDate = uaRe.ReplaceAllString(ua, "${fw_date}")
			h.FirmwareId = uaRe.ReplaceAllString(ua, "${fw_id}")
			h.Model = uaRe.ReplaceAllString(ua, "${model}")
		}

		// var t any
		// err := json.NewDecoder(r.Body).Decode(&t)
		// if err != nil {
		// 	http.Error(w, err.Error(), http.StatusBadRequest)
		// 	return
		// }
		// tc <- req.Body

		ip, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			//return nil, fmt.Errorf("userip: %q is not IP:port", req.RemoteAddr)
			log.Default().Printf("userip: %q is not IP:port", req.RemoteAddr)
		}
		h.Ip = net.ParseIP(ip)
		if h.Ip == nil {
			//return nil, fmt.Errorf("userip: %q is not IP:port", req.RemoteAddr)
			log.Default().Printf("userip: %q is not IP:port", req.RemoteAddr)
			return
		}

		log.Default().Printf("hook.HTSensor(struct): %v", h.HTSensor)
		tc <- h

		jd, _ := json.Marshal(h)
		log.Default().Printf("hook.HTSensor(JSON): %v", string(jd))

		_, _ = w.Write([]byte("")) // 200 OK
	})
	log.Default().Print("Now listening on port 8888.")
	http.ListenAndServe(":8888", nil)
}