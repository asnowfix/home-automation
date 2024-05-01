package gen1

import (
	"devices"
	"devices/shelly/temperature"
	"encoding/json"
	"fmt"
	"log"
)

type Empty struct{}

func Publisher(ch chan Device, tc chan string) {
	for device := range ch {
		var tC float32
		var id string
		if device.HTSensor != nil {
			tC = device.HTSensor.Temperature
			id = device.HTSensor.Id
		}
		if device.Flood != nil {
			tC = device.Flood.Temperature
			id = device.Flood.Id
		}
		t := temperature.Status{
			Id:         0,
			Celsius:    tC,
			Fahrenheit: (tC * 1.8) + 32.0,
		}
		// https://shelly-api-docs.shelly.cloud/gen2/General/RPCChannels#mqtt
		topic := fmt.Sprintf("%v/events/rpc", id)
		tc <- topic
		msg, _ := json.Marshal(t)
		log.Default().Printf("gen1.Publisher: MQTT(%v) <<< %v", topic, string(msg))
		devices.MqttClient().Publish(topic, 1 /*qos:at-least-once*/, true /*retain*/, string(msg)).Wait()
	}
}
