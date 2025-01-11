package gen1

import (
	"encoding/json"
	"fmt"
	"mymqtt"
	"pkg/shelly/temperature"

	"github.com/go-logr/logr"
)

type Empty struct{}

func Publisher(log logr.Logger, ch chan Device, mc *mymqtt.Client) {
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
		msg, _ := json.Marshal(t)
		log.Info("gen1.Publisher: MQTT(%v) <<< %v", topic, string(msg))
		mc.Publish(topic, msg)
	}
}
