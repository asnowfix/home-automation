package gen1

import (
	"devices"
	"devices/shelly/temperature"
	"encoding/json"
	"fmt"
	"log"
	"reflect"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Empty struct{}

func Publisher(ch chan Device) {
	opts := devices.CreateClientOptions(reflect.TypeOf(Empty{}).PkgPath())
	client := mqtt.NewClient(opts)

	for {
		device := <-ch
		t := temperature.Status{
			Id:         0,
			Celsius:    device.Temperature,
			Fahrenheit: (device.Temperature * 1.8) + 32.0,
		}
		topic := fmt.Sprintf("device/%v", device.Id)
		msg, _ := json.Marshal(t)
		log.Default().Printf("%v <<< %v", topic, string(msg))
		client.Publish(topic, 0, false, string(msg))
	}

}
