package mqtt

import (
	"devices"
	"encoding/json"
	"fmt"
	"log"
	"mqtt"
	"reflect"
	"sync"
	"time"
)

func CommandProxy(run chan struct{}) {

	subscriptions := make(map[string]func(mqtt.MqttMessage))
	subscriptions["devices/status"] = handleDevicesStatus

	subsch := make(map[string]chan mqtt.MqttMessage)
	for topic, handler := range subscriptions {
		// Subscribe to the topic
		log.Default().Print("Subscribing to topic ", topic)
		subsch[topic], _ = mqtt.MqttSubscribe(mqtt.Broker(true), topic, 0)
		go func(topic string, handler func(mqtt.MqttMessage)) {
			for {
				select {
				// In case of channel close, exit the loop
				case <-subsch[topic]:
					log.Default().Print("Unsubscribing from topic ", topic)
					mqtt.MqttClient(mqtt.Broker(true)).Unsubscribe(topic).Wait()
					return
				// In case of message, handle it
				case msg := <-subsch[topic]:
					log.Default().Print("Received message on topic ", topic)
					handler(msg)
				}
			}
		}(topic, handler)
	}

	// Asynchronous recurring loop to refresh hosts
	refresh := make(chan struct{}, 1)
	go func() {
		select {
		case <-refresh:
			return
		case <-time.After(60 * time.Second):
			refreshHosts()
		}
	}()

	// Wait for disconnection from run channel
	<-run

	// Tell the refresh loop to stop
	log.Default().Print("Closing refresh channel")
	refresh <- struct{}{}

	for topic, subch := range subsch {
		log.Default().Print("Closing subscription channel for topic", topic)
		close(subch)
	}
}

var hostsLock sync.Mutex

var hosts []devices.Host

func refreshHosts() {
	hostsLock.Lock()
	defer hostsLock.Unlock()

	var err error
	hosts, err = devices.List()
	if err != nil {
		log.Default().Print(err)
		return
	}
	log.Default().Print(hosts)
}

func handleDevicesStatus(msg mqtt.MqttMessage) {
	hostsLock.Lock()
	defer hostsLock.Unlock()

	var req struct {
		ClientId  string `json:"client_id"`
		RequestId string `json:"request_id"`
	}
	err := json.Unmarshal(msg.Payload, &req)
	if err != nil {
		log.Default().Print(err)
		return
	}

	var res struct {
		RequestId string        `json:"request_id"`
		Error     string        `json:"error,omitempty"`
		Devices   []interface{} `json:"devices,omitempty"`
	}
	res.RequestId = req.RequestId

	log.Default().Printf("Found %v devices '%v'\n", len(hosts), reflect.TypeOf(hosts))
	out, err := json.Marshal(hosts)
	if err != nil {
		handleError(req.ClientId, req.RequestId, err)
		return
	}
	mqtt.MqttPublish(mqtt.Broker(true), fmt.Sprintf("client/%v", req.ClientId), out)
}

func handleError(clientId string, requestId string, err error) {
	var res struct {
		RequestId string `json:"request_id"`
		Error     string `json:"error,omitempty"`
	}
	res.RequestId = requestId
	res.Error = err.Error()
	out, err := json.Marshal(res)
	if err != nil {
		log.Default().Print(err)
		return
	}
	mqtt.MqttPublish(mqtt.Broker(true), fmt.Sprintf("client/%v", clientId), out)
}
