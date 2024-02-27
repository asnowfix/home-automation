package devices

import (
	"fmt"
	"log"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/hashicorp/mdns"
)

func connect(clientId string) mqtt.Client {
	opts := CreateClientOptions(clientId)
	client := mqtt.NewClient(opts)
	token := client.Connect()
	for !token.WaitTimeout(3 * time.Second) {
	}
	if err := token.Error(); err != nil {
		log.Fatal(err)
	}
	return client
}

func CreateClientOptions(clientId string) *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s", MqttBroker()))
	opts.SetUsername(MqttUsername)
	opts.SetPassword(MqttPassword)
	opts.SetClientID(clientId)
	return opts
}

var MqttUsername string = ""

var MqttPassword string = ""

const MqttService = "_mqtt._tcp"

var mqttBroker string

func MqttBroker() string {
	if len(mqttBroker) == 0 {
		ch := make(chan *mdns.ServiceEntry, 4)

		go func() {
			for entry := range ch {
				// Filter-out spurious candidates
				if strings.Contains(entry.Name, MqttService) {
					mqttBroker = fmt.Sprintf("%v:%v", entry.Addr, entry.Port)
				}
			}
		}()

		// Start the lookup
		mdns.Lookup(MqttService, ch)
		close(ch)

		log.Default().Printf("Using MQTT broker %v for service %v", mqttBroker, MqttService)
	}
	return mqttBroker
}

type MqttMessage struct {
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

func MqttSubscribe(clientId string, topic string, qlen uint) (chan MqttMessage, error) {
	mch := make(chan MqttMessage, qlen)

	go func() {
		client := connect(clientId)
		client.Subscribe(topic, 0, func(client mqtt.Client, msg mqtt.Message) {
			log.Default().Printf("< [%s] %s", msg.Topic(), string(msg.Payload()))
			mch <- MqttMessage{
				Topic:   msg.Topic(),
				Payload: msg.Payload(),
			}
		})
	}()

	return mch, nil
}

func MqttPublish(clientId string, topic string, msg []byte) {
	client := connect(clientId)
	log.Default().Printf("> [%s] %s", topic, string(msg))
	client.Publish(topic, 0, false, msg)
}
