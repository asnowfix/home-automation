package mqtt

import (
	"devices/shelly"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"log"
	"mqtt"
	"net/http"
	"os"
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

	shelly.RegisterDeviceCaller(shelly.Mqtt, shelly.DeviceCaller(callDevice))
}

func callDevice(device *shelly.Device, verb types.MethodHandler, method string, out any, params any) (any, error) {
	reqTopic := fmt.Sprintf(" %v/rpc", device.Id)
	// reqChan, err := mqtt.MqttSubscribe(mqtt.PrivateBroker(), reqTopic, uint(AtLeastOnce))
	var req struct {
		Source string `json:"src"`
		Id     uint   `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Default().Printf("Unable to get local hostname: %v", err)
		return nil, err
	}
	req.Source = fmt.Sprintf("%v_%v", hostname, os.Getpid())
	req.Id = 0
	req.Method = method
	req.Params = params

	resChan, err := mqtt.MqttSubscribe(mqtt.Broker(false), fmt.Sprintf(" %v/rpc", req.Source), uint(AtLeastOnce))
	if err != nil {
		log.Default().Printf("Unable to subscribe to topic '%v': %v", reqTopic, err)
		return nil, err
	}

	reqPayload, err := json.Marshal(req)
	if err != nil {
		log.Default().Printf("Unable to marshal request payload '%v': %v", req, err)
		return nil, err
	}

	mqtt.MqttPublish(mqtt.Broker(false), reqTopic, reqPayload)
	res := <-resChan

	err = json.Unmarshal(res.Payload, &out)
	if err != nil {
		log.Default().Printf("Unable to unmarshal response payload '%v': %v", res, err)
		return nil, err
	}

	return out, nil
}

func Setup(device *shelly.Device) (*shelly.Device, error) {
	config := shelly.Call(device, "Mqtt", "GetConfig", nil).(*Configuration)
	configStr, _ := json.Marshal(config)
	log.Default().Printf("initial MQTT config: %v", string(configStr))

	status := shelly.Call(device, "Mqtt", "GetStatus", nil).(*Status)
	statusStr, _ := json.Marshal(status)
	log.Default().Printf("initial MQTT status: %v", string(statusStr))

	config.Enable = true
	config.RpcNotifs = true
	config.StatusNotifs = true
	broker := mqtt.Broker(false)
	// Shelly MQTT Server is formatted like host:port
	config.Server = fmt.Sprintf("%s:%s", broker.Hostname(), broker.Port())

	configStr, _ = json.Marshal(config)
	log.Default().Printf("new MQTT config: %v", string(configStr))

	res := shelly.Call(device, "Mqtt", "SetConfig", config).(*ConfigResults)
	if res.Result.RestartRequired {
		_, err := shelly.CallE(device, "Shelly", "Reboot", config)
		return device, err
	}
	return device, nil
}
