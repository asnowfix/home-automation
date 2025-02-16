package gen1

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"pkg/shelly/temperature"

	"github.com/go-logr/logr"
)

type Empty struct{}

func Publisher(ctx context.Context, log logr.Logger, ch chan Device, mc *mymqtt.Client) {
	publishers := make(map[string]chan<- []byte)
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
		msg, _ := json.Marshal(t)

		// https://shelly-api-docs.shelly.cloud/gen2/General/RPCChannels#mqtt
		topic := fmt.Sprintf("%v/events/rpc", id)
		if _, exists := publishers[id]; !exists {
			publisher, err := mc.Publisher(ctx, topic, 1 /*qlen*/)
			if err != nil {
				log.Error(err, "Unable to create pseudo publisher", "topic", topic)
			}
			publishers[id] = publisher
		}
		log.Info("gen1.Publish", "topic", topic, "msg", string(msg))
		publishers[id] <- msg
	}
}
