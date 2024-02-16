package http

import (
	"devices/shelly/gen1"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"

	"github.com/gorilla/schema"
)

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
