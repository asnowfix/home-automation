package mymqtt

import (
	"context"
	"fmt"
	"mynet"
	"net"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const BROKER_SERVICE = "_mqtt._tcp."
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
			log.Info("Using local IP as MQTT broker")
			_, lip, err := mynet.MainInterface(log)
			if err != nil {
				log.Error(err, "Could not get local IP")
				panic(err)
			}
			ip = lip
		} else {
			log.Info("Looking up MQTT broker")
			ips, err := lookupBroker(log)
			if err == nil && len(ips) > 0 {
				ip = &ips[0]
			} else {
				log.Error(err, "Zeroconf lookup failed", "service", BROKER_SERVICE)
				_broker, err = zeroconfBroker(log)
				if err != nil {
					log.Error(err, "Zeroconf broker lookup failed")
					panic(err)
				}
			}
		}
		_broker = &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", ip, PRIVATE_PORT),
		}
	}
	return _broker
}

func lookupBroker(log logr.Logger) ([]net.IP, error) {
	log.Info("Looking up via Zeroconf", "service", BROKER_SERVICE)

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Error(err, "Failed to initialize Zeroconf resolver")
		return nil, err
	}

	ips := make([]net.IP, 0)
	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		// until the channel is closed
		for entry := range entries {
			log.Info("Found from Zeroconf", "entry", entry)
			ips = append(ips, entry.AddrIPv4...)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	err = resolver.Browse(ctx, BROKER_SERVICE, "", entries)
	if err != nil {
		log.Error(err, "Failed to browse")
		return nil, err
	}

	<-ctx.Done()
	return ips, nil
}

var mqttClient mqtt.Client = nil

var mutexClient sync.Mutex

func MqttClient(log logr.Logger, broker *url.URL) mqtt.Client {
	if mqttClient, err := MqttClientE(log, broker); err != nil {
		log.Error(err, "Failed to initialize MQTT client")
		panic(err)
	} else {
		return mqttClient
	}
}

func MqttClientE(log logr.Logger, broker *url.URL) (mqtt.Client, error) {
	mutexClient.Lock()
	defer mutexClient.Unlock()

	if mqttClient == nil {
		clientId := fmt.Sprintf("%v:%v", path.Base(os.Args[0]), os.Getpid())
		log.Info("Initializing MQTT client", "client_id", clientId)

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
			log.Info("Using MQTT", "broker", broker.String())
			opts.AddBroker(broker.String())
			opts.Servers = []*url.URL{broker}
		}

		// Connect to the MQTT broker
		mqttClient = mqtt.NewClient(opts)
		token := mqttClient.Connect()
		for !token.WaitTimeout(3 * time.Second) {
			log.Info("MQTT client trying to connect as", "client_id", clientId)
		}
		if err := token.Error(); err != nil {
			log.Error(err, "MQTT client failed to connect", "client_id", clientId)
			return nil, err
		}
		log.Info("MQTT client connected", "client_id", clientId)
	}
	return mqttClient, nil
}

var MqttUsername string = ""

var MqttPassword string = ""

const ZEROCONF_SERVICE = "_mqtt._tcp."

var brokers []*url.URL = make([]*url.URL, 0)

func zeroconfBroker(log logr.Logger) (*url.URL, error) {
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
		log.Error(err, "failed to browse")
		return nil, err
	}

	// wait for the lookup to complete
	<-ctx.Done()

	log.Info("Using MQTT", "broker", brokers, "service", ZEROCONF_SERVICE)
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no MQTT broker found")
	} else {
		return brokers[0], nil
	}
}

type MqttMessage struct {
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

func MqttSubscribe(log logr.Logger, broker *url.URL, topic string, qlen uint) (chan MqttMessage, error) {
	mch := make(chan MqttMessage, qlen)

	log.Info("Subscribing to:", "topic", topic)
	MqttClient(log, broker).Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
		go func() {
			log.Info("Received from MQTT:", "topic", msg.Topic(), "payload", string(msg.Payload()))
			mch <- MqttMessage{
				Topic:   msg.Topic(),
				Payload: msg.Payload(),
			}
		}()
	})
	log.Info("Subscribed to:", "topic", topic)

	return mch, nil
}

func MqttUnsubscribe(log logr.Logger, broker *url.URL, topic string) {
	log.Info("Unsubscribing:", "topic", topic)
	MqttClient(log, broker).Unsubscribe(topic)
}

func MqttPublish(log logr.Logger, broker *url.URL, topic string, msg []byte) {
	log.Info("Publishing:", "topic", topic, "payload", string(msg))
	MqttClient(log, broker).Publish(topic, 0, false, msg)
	log.Info("Published:", "topic", topic, "payload", string(msg))
}
