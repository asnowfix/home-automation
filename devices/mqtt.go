package devices

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/grandcat/zeroconf"
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
		resolver, err := zeroconf.NewResolver(nil)
		if err != nil {
			log.Fatalln("Failed to initialize resolver:", err.Error())
		}

		entries := make(chan *zeroconf.ServiceEntry)

		go func() {
			for entry := range entries {
				// Filter-out spurious candidates
				if strings.Contains(entry.Service, MqttService) {
					mqttBroker = fmt.Sprintf("%v:%v", entry.AddrIPv4, entry.Port)
				}
			}
		}()

		// Start the lookup
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
		defer cancel()
		err = resolver.Browse(ctx, MqttService, "local.", entries)
		if err != nil {
			log.Fatalln("Failed to browse:", err.Error())
		}

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
			log.Default().Printf("MqttSubscribe: MQTT(%s) >>> %s", msg.Topic(), string(msg.Payload()))
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
	log.Default().Printf("MqttPublish: MQTT(%s) <<< %s", topic, string(msg))
	client.Publish(topic, 0, false, msg)
}
