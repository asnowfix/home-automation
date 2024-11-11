package mqtt

import (
	"devices"
	"encoding/json"
	"fmt"
	"mymqtt"
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

func CommandProxy(log logr.Logger, run chan struct{}) {

	subscriptions := make(map[string]func(logr.Logger, mymqtt.MqttMessage))
	subscriptions["devices/status"] = handleDevicesStatus

	subsch := make(map[string]chan mymqtt.MqttMessage)
	for topic, handler := range subscriptions {
		// Subscribe to the topic
		log.Info("Subscribing", "topic", topic)
		subsch[topic], _ = mymqtt.MqttSubscribe(log, mymqtt.Broker(log, true), topic, 0)
		go func(topic string, handler func(logr.Logger, mymqtt.MqttMessage)) {
			for {
				select {
				// In case of channel close, exit the loop
				case <-subsch[topic]:
					log.Info("Unsubscribing", "topic", topic)
					mymqtt.MqttClient(log, mymqtt.Broker(log, true)).Unsubscribe(topic).Wait()
					return
				// In case of message, handle it
				case msg := <-subsch[topic]:
					log.Info("Received message", "topic ", topic, "msg", string(msg.Payload))
					handler(log, msg)
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
			refreshHosts(log)
		}
	}()

	// Wait for disconnection from run channel
	<-run

	// Tell the refresh loop to stop
	log.Info("Closing refresh channel")
	refresh <- struct{}{}

	for topic, subch := range subsch {
		log.Info("Closing subscription channel for topic", topic)
		close(subch)
	}
}

var hostsLock sync.Mutex

var hosts []devices.Host

func refreshHosts(log logr.Logger) {
	hostsLock.Lock()
	defer hostsLock.Unlock()

	var err error
	hosts, err = devices.List(log)
	if err != nil {
		log.Error(err, "Unable to list devices")
		return
	}
	log.Info("hosts", hosts)
}

func handleDevicesStatus(log logr.Logger, msg mymqtt.MqttMessage) {
	hostsLock.Lock()
	defer hostsLock.Unlock()

	var req struct {
		ClientId  string `json:"client_id"`
		RequestId string `json:"request_id"`
	}
	err := json.Unmarshal(msg.Payload, &req)
	if err != nil {
		log.Error(err, "Unable to unmarshal message")
		return
	}

	var res struct {
		RequestId string        `json:"request_id"`
		Error     string        `json:"error,omitempty"`
		Devices   []interface{} `json:"devices,omitempty"`
	}
	res.RequestId = req.RequestId

	log.Info("Found devices", "len", len(hosts), "type", reflect.TypeOf(hosts))
	out, err := json.Marshal(hosts)
	if err != nil {
		handleError(log, req.ClientId, req.RequestId, err)
		return
	}
	mymqtt.MqttPublish(log, mymqtt.Broker(log, true), fmt.Sprintf("client/%v", req.ClientId), out)
}

func handleError(log logr.Logger, clientId string, requestId string, err error) {
	var res struct {
		RequestId string `json:"request_id"`
		Error     string `json:"error,omitempty"`
	}
	res.RequestId = requestId
	res.Error = err.Error()
	out, err := json.Marshal(res)
	if err != nil {
		log.Error(err, "Unable to marshal error response")
		return
	}
	mymqtt.MqttPublish(log, mymqtt.Broker(log, true), fmt.Sprintf("client/%v", clientId), out)
}
