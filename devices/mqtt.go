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

func MqttClient(broker *url.URL) mqtt.Client {
	mutexClient.Lock()
	defer mutexClient.Unlock()

	if mqttClient == nil {
		clientId := fmt.Sprintf("%v:%v", os.Args[0], os.Getpid())
		log.Default().Printf("Initializing MQTT client %v", clientId)

		opts := mqtt.NewClientOptions()
		opts.SetUsername(MqttUsername)
		opts.SetPassword(MqttPassword)
		opts.SetClientID(clientId)

		if broker == nil {
			// Finding brokers
			brokers := MqttBrokers()
			if len(brokers) == 0 {
				log.Fatal("No MQTT broker found")
			}
			log.Default().Printf("Found %v MQTT brokers", len(brokers))

			// for each broker, add it to the list of servers
			for _, broker := range brokers {
				log.Default().Printf("Adding MQTT broker %s", broker.String())
				opts.AddBroker(broker.String())
			}
			opts.Servers = brokers
		} else {
			log.Default().Printf("Using MQTT broker '%s'", broker.String())
			opts.AddBroker(broker.String())
			opts.Servers = []*url.URL{broker}
		}

		// Connect to the MQTT broker
		mqttClient := mqtt.NewClient(opts)
		token := mqttClient.Connect()
		for !token.WaitTimeout(3 * time.Second) {
			log.Default().Printf("Waiting for MQTT client %v to connect", clientId)
		}
		if err := token.Error(); err != nil {
			log.Fatal(err)
		}
		log.Default().Printf("Connected MQTT client %v (%v)", clientId, mqttClient)
	}
	log.Default().Printf("Using connected MQTT client %v", mqttClient)
	return mqttClient
}

var MqttUsername string = ""

var MqttPassword string = ""

const MqttService = "_mqtt._tcp"

var brokerMutex sync.Mutex

var brokers []*url.URL = make([]*url.URL, 0)

func MqttBrokers() []*url.URL {
	brokerMutex.Lock()
	defer brokerMutex.Unlock()

	if len(brokers) > 0 {
		return brokers
	}

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatalln("Failed to initialize resolver:", err.Error())
	}

	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			// Filter-out spurious candidates
			if strings.Contains(entry.Service, MqttService) {
				log.Default().Printf("Found MQTT broker %v:%v", entry.AddrIPv4, entry.Port)
				for _, addrIpV4 := range entry.AddrIPv4 {
					// Append the MQTT broker URL format host:port to known brokers
					brokers = append(brokers, &url.URL{
						Scheme: "tcp",
						Host:   fmt.Sprintf("%v:%v", addrIpV4, entry.Port),
					})
				}
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

	log.Default().Printf("Using MQTT broker %v for service %v", brokers, MqttService)
	return brokers
}

type MqttMessage struct {
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

func MqttSubscribe(broker *url.URL, topic string, qlen uint) (chan MqttMessage, error) {
	mch := make(chan MqttMessage, qlen)

	go func() {
		log.Default().Printf("MqttSubscribe: subscribing to %s", topic)
		MqttClient(broker).Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
			log.Default().Printf("MqttSubscribe: MQTT(%s) >>> %s", msg.Topic(), string(msg.Payload()))
			mch <- MqttMessage{
				Topic:   msg.Topic(),
				Payload: msg.Payload(),
			}
		})
	}()

	return mch, nil
}

func MqttPublish(broker *url.URL, topic string, msg []byte) {
	log.Default().Printf("MqttPublish: MQTT(%s) <<< %s", topic, string(msg))
	MqttClient(broker).Publish(topic, 0, false, msg)
}
