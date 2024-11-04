package mymqtt

import (
	"context"
	"fmt"
	"mynet"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const BROKER_HOSTNAME = "mqtt"
const PRIVATE_PORT = 1883
const PUBLIC_PORT = 8883

var _broker *url.URL

var _brokerMutex sync.Mutex

func Broker(log logr.Logger, myself bool) *url.URL {
	_brokerMutex.Lock()
	defer _brokerMutex.Unlock()

	if _broker == nil {
		var ip *net.IP
		if myself {
			_, lip, err := mynet.MainInterface(log)
			if err != nil {
				log.Error(err, "Could not get local IP")
			}
			ip = lip
		} else {
			ips, err := net.LookupIP(BROKER_HOSTNAME)
			if err == nil {
				ip = &ips[0]
			} else {
				log.Info("Could not find IPs for host %v: %v", BROKER_HOSTNAME, err)
				_broker = zeronconfBroker(log)
			}
		}
		_broker = &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", ip, PRIVATE_PORT),
		}
	}
	return _broker
}

var mqttClient mqtt.Client = nil

var mutexClient sync.Mutex

func MqttClient(log logr.Logger, broker *url.URL) mqtt.Client {
	mutexClient.Lock()
	defer mutexClient.Unlock()

	if mqttClient == nil {
		clientId := fmt.Sprintf("%v:%v", os.Args[0], os.Getpid())
		log.Info("Initializing MQTT client %v", clientId)

		opts := mqtt.NewClientOptions()
		opts.SetUsername(MqttUsername)
		opts.SetPassword(MqttPassword)
		opts.SetClientID(clientId)

		if broker == nil {
			broker := Broker(log, false)
			opts.AddBroker(broker.String())
			opts.Servers = make([]*url.URL, 1)
			opts.Servers[0] = broker
		} else {
			log.Info("Using MQTT broker '%s'", broker.String())
			opts.AddBroker(broker.String())
			opts.Servers = []*url.URL{broker}
		}

		// Connect to the MQTT broker
		mqttClient := mqtt.NewClient(opts)
		token := mqttClient.Connect()
		for !token.WaitTimeout(3 * time.Second) {
			log.Info("Waiting for MQTT client %v to connect", clientId)
		}
		if err := token.Error(); err != nil {
			log.Error(err, "Failed to connect MQTT client %v (%v)", clientId, mqttClient)
		}
		log.Info("Connected MQTT client %v (%v)", clientId, mqttClient)
	}
	log.Info("Using connected MQTT client %v", mqttClient)
	return mqttClient
}

var MqttUsername string = ""

var MqttPassword string = ""

const ZEROCONF_SERVICE = "_mqtt._tcp"

var brokers []*url.URL = make([]*url.URL, 0)

func zeronconfBroker(log logr.Logger) *url.URL {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Error(err, "Failed to initialize zeronconf resolver:")
	}

	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			// Filter-out spurious candidates
			if strings.Contains(entry.Service, ZEROCONF_SERVICE) {
				log.Info("Found MQTT broker %v:%v", entry.AddrIPv4, entry.Port)
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	err = resolver.Browse(ctx, ZEROCONF_SERVICE, "local.", entries)
	if err != nil {
		log.Error(err, "Failed to browse")
	}

	// wait for the lookup to complete
	<-ctx.Done()

	log.Info("Using MQTT broker %v for service %v", brokers, ZEROCONF_SERVICE)
	return brokers[0]
}

type MqttMessage struct {
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

func MqttSubscribe(log logr.Logger, broker *url.URL, topic string, qlen uint) (chan MqttMessage, error) {
	mch := make(chan MqttMessage, qlen)

	go func() {
		log.Info("MqttSubscribe: subscribing to %s", topic)
		MqttClient(log, broker).Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
			log.Info("MqttSubscribe: MQTT(%s) >>> %s", msg.Topic(), string(msg.Payload()))
			mch <- MqttMessage{
				Topic:   msg.Topic(),
				Payload: msg.Payload(),
			}
		})
	}()

	return mch, nil
}

func MqttPublish(log logr.Logger, broker *url.URL, topic string, msg []byte) {
	log.Info("MqttPublish: MQTT(%s) <<< %s", topic, string(msg))
	MqttClient(log, broker).Publish(topic, 0, false, msg)
}
