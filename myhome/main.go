package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"devices"

	"github.com/gorilla/schema"
	"github.com/hashicorp/mdns"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

var Program string
var Version string
var Commit string

func main() {
	// Create signals channel to run server until interrupted
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	// create signal channel to receive temperature inputs
	// tc := make(chan any, 10)
	go func() {
		type HTSensor struct {
			Humidity    uint    `schema:"hum,required"  json:"humidity"`
			Temperature float32 `schema:"temp,required" json:"temperature"`
			DeviceId    string  `schema:"id,required"   json:"id"`
		}
		var hook struct {
			*HTSensor
		}

		var decoder = schema.NewDecoder()

		http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			log.Default().Printf("url: %s", req.URL)

			m, _ := url.ParseQuery(req.URL.RawQuery)
			log.Default().Printf("query: %v", m)

			err := decoder.Decode(&hook, m)
			if err != nil {
				log.Default().Print(err)
				return
			}
			log.Default().Printf("hook.HTSensor(struct): %v", hook.HTSensor)

			jd, _ := json.Marshal(hook)
			log.Default().Printf("hook.HTSensor(JSON): %v", string(jd))

			for k, v := range req.Header {
				log.Default().Printf("header: %s: %s", k, v)
			}

			// var t any
			// err := json.NewDecoder(r.Body).Decode(&t)
			// if err != nil {
			// 	http.Error(w, err.Error(), http.StatusBadRequest)
			// 	return
			// }
			// tc <- req.Body
			_, _ = w.Write([]byte("")) // 200 OK
		})
		log.Default().Print("Now listening on port 8888.")
		http.ListenAndServe(":8888", nil)
	}()

	// Create the new MQTT Server.
	mqttServer := mqtt.New(nil)

	// Allow all connections.
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// Create a TCP listener on a standard port.
	tcp := listeners.NewTCP("t1", ":1883", nil)
	err := mqttServer.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}

	// Publish over mDNS
	host, _ := os.Hostname()
	info := []string{
		fmt.Sprintf("hostname=%v", host),
		fmt.Sprintf("program=%v", Program),
		fmt.Sprintf("version=%v", Version),
		fmt.Sprintf("commit=%v", Commit),
	}
	mdnsService, _ := mdns.NewMDNSService(host, devices.MqttService, "", "", 1883, nil, info)
	log.Default().Printf("publishing %v as %v over mDNS", info, devices.MqttService)

	// Create the mDNS server, defer shutdown
	mdnsServer, _ := mdns.NewServer(&mdns.Config{Zone: mdnsService})
	defer mdnsServer.Shutdown()

	go func() {
		err := mqttServer.Serve()
		if err != nil {
			log.Fatal(err)
		}
	}()

	// Run server until interrupted
	<-done

	// Cleanup
}
