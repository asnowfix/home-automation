package devices

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/grandcat/zeroconf"
)

var mqttClient mqtt.Client = nil

var mutexClient sync.Mutex

func MqttClient() mqtt.Client {
	mutexClient.Lock()
	if mqttClient == nil {
		clientId := fmt.Sprintf("%v:%v", os.Args[0], os.Getpid())
		log.Default().Printf("initializing MQTT client %v", clientId)

		opts := mqtt.NewClientOptions()
		opts.AddBroker(fmt.Sprintf("tcp://%s", MqttBroker()))
		opts.SetUsername(MqttUsername)
		opts.SetPassword(MqttPassword)
		opts.SetClientID(clientId)

		opts.Servers = MqttBroker()

		mqttClient := mqtt.NewClient(opts)
		token := mqttClient.Connect()
		for !token.WaitTimeout(3 * time.Second) {
		}
		if err := token.Error(); err != nil {
			log.Fatal(err)
		}
		log.Default().Printf("connected MQTT client %v (%v)", clientId, mqttClient)
	}
	mutexClient.Unlock()
	log.Default().Printf("using connected MQTT client %v", mqttClient)
	return mqttClient
}

var MqttUsername string = ""

var MqttPassword string = ""

const MqttService = "_mqtt._tcp"

func MqttBroker() []*url.URL {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatalln("Failed to initialize resolver:", err.Error())
	}

	entries := make(chan *zeroconf.ServiceEntry)
	mu := make([]*url.URL, 0)

	go func() {
		for entry := range entries {
			// Filter-out spurious candidates
			if strings.Contains(entry.Service, MqttService) {
				// Append the MQTT broker URL format host:port to mu
				mu = append(mu, &url.URL{
					Scheme: "tcp",
					Host:   fmt.Sprintf("%v:%v", entry.AddrIPv4, entry.Port),
				})
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

	// wait for the lookup to complete
	<-ctx.Done()

	log.Default().Printf("Using MQTT broker %v for service %v", mu, MqttService)
	return mu
}

type MqttMessage struct {
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

func MqttSubscribe(topic string, qlen uint) (chan MqttMessage, error) {
	mch := make(chan MqttMessage, qlen)

	go func() {
		log.Default().Printf("MqttSubscribe: subscribing to %s", topic)
		MqttClient().Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
			log.Default().Printf("MqttSubscribe: MQTT(%s) >>> %s", msg.Topic(), string(msg.Payload()))
			mch <- MqttMessage{
				Topic:   msg.Topic(),
				Payload: msg.Payload(),
			}
		})
	}()

	return mch, nil
}

func MqttPublish(topic string, msg []byte) {
	log.Default().Printf("MqttPublish: MQTT(%s) <<< %s", topic, string(msg))
	MqttClient().Publish(topic, 0, false, msg)
}
