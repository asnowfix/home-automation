package mqtt

import (
	"devices"
	"devices/shelly"
	"devices/shelly/types"
	"encoding/json"
	"log"
	"net/http"
)

func init() {
	shelly.RegisterMethodHandler("Mqtt", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	shelly.RegisterMethodHandler("Mqtt", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	shelly.RegisterMethodHandler("Mqtt", "SetConfig", types.MethodHandler{
		Allocate: func() any { return new(ConfigResults) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodPost,
	})
}

func Setup(device *shelly.Device) (*shelly.Device, error) {
	config := shelly.Call(device, "Mqtt", "GetConfig", nil).(*Configuration)
	configStr, _ := json.Marshal(config)
	log.Default().Printf("initial MQTT config: %v", string(configStr))

	status := shelly.Call(device, "Mqtt", "GetStatus", nil).(*Status)
	statusStr, _ := json.Marshal(status)
	log.Default().Printf("initial MQTT status: %v", string(statusStr))

	config.Enable = true
	config.Server = devices.MqttBroker()
	config.RpcNotifs = true
	config.StatusNotifs = true

	configStr, _ = json.Marshal(config)
	log.Default().Printf("new MQTT config: %v", string(configStr))

	res := shelly.Call(device, "Mqtt", "SetConfig", config).(*ConfigResults)
	if res.Result.RestartRequired {
		_, err := shelly.CallE(device, "Shelly", "Reboot", config)
		return device, err
	}
	return device, nil
}
