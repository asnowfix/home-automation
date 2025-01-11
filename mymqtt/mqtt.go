package mymqtt

import (
	"context"
	"fmt"
	"mynet"
	"net"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const BROKER_SERVICE = "_mqtt._tcp."
const PRIVATE_PORT = 1883
const PUBLIC_PORT = 8883

type Client struct {
	mqtt      mqtt.Client
	brokerUrl *url.URL
	log       logr.Logger
}

func NewClientE(log logr.Logger, where string) (*Client, error) {
	clientId := fmt.Sprintf("%v:%v", path.Base(os.Args[0]), os.Getpid())
	log.Info("Initializing MQTT client", "client_id", clientId)

	opts := mqtt.NewClientOptions()
	opts.SetUsername(MqttUsername)
	opts.SetPassword(MqttPassword)
	opts.SetClientID(clientId)

	brokerUrl, err := lookupBroker(log, where)
	if err != nil {
		log.Error(err, "could not find MQTT broker", "where", where)
		return nil, err
	}
	opts.AddBroker(brokerUrl.String())
	opts.Servers = []*url.URL{brokerUrl}

	client := Client{
		mqtt:      mqtt.NewClient(opts),
		log:       log,
		brokerUrl: brokerUrl,
	}
	token := client.mqtt.Connect()
	for !token.WaitTimeout(3 * time.Second) {
		log.Info("MQTT client trying to connect as", "client_id", clientId)
	}
	if err := token.Error(); err != nil {
		log.Error(err, "MQTT client failed to connect", "client_id", clientId)
		return nil, err
	}
	log.Info("MQTT client connected", "client_id", clientId)

	return &client, nil
}

func lookupBroker(log logr.Logger, where string) (*url.URL, error) {
	log.Info("Looking up MQTT broker", "where", where)

	var ip net.IP

	if where == "me" {
		log.Info("Finding local IP")
		_, ip, err := mynet.MainInterface(log)
		if err != nil {
			log.Error(err, "Could not get local IP")
			return nil, err
		}
		log.Info("Using", "ip", ip)
		return &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", ip, PRIVATE_PORT),
		}, nil
	}

	p := strings.Split(where, ":")
	if len(p) == 2 {
		ip = net.ParseIP(p[0])
		_, err := strconv.Atoi(p[1])
		if ip != nil && err == nil {
			log.Info("Using", "ip", ip)
			return &url.URL{
				Scheme: "tcp",
				Host:   where,
			}, nil
		}
		log.Error(fmt.Errorf("invalid IP:port %v", where), "Ignoring")
		return nil, fmt.Errorf("invalid IP:port %v", where)
	}

	url, err := lookupBrokerViaZeroConf(log)
	if err != nil {
		log.Error(err, "Zeroconf lookup failed", "service", BROKER_SERVICE)
		return nil, err
	}
	return url, nil
}

func (c *Client) BrokerUrl() *url.URL {
	return c.brokerUrl
}

func (c *Client) Close() {
	c.mqtt.Disconnect(250 /* milliseconds */)
}

var MqttUsername string = ""

var MqttPassword string = ""

const ZEROCONF_SERVICE = "_mqtt._tcp."

func lookupBrokerViaZeroConf(log logr.Logger) (*url.URL, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Error(err, "Failed to initialize zeronconf resolver:")
	}

	entries := make(chan *zeroconf.ServiceEntry)
	brokers := make([]*url.URL, 0)

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

func (c *Client) Subscribe(topic string, qlen uint) (chan []byte, error) {
	mch := make(chan []byte, qlen)

	c.log.Info("Subscribing to:", "topic", topic)
	c.mqtt.Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
		go func() {
			c.log.Info("Received from MQTT:", "topic", msg.Topic(), "payload", string(msg.Payload()))
			mch <- msg.Payload()
		}()
	})
	c.log.Info("Subscribed to:", "topic", topic)

	return mch, nil
}

func (c *Client) Unsubscribe(topic string) {
	c.log.Info("Unsubscribing:", "topic", topic)
	c.mqtt.Unsubscribe(topic)
}

func (c *Client) Publish(topic string, msg []byte) {
	c.log.Info("Publishing:", "topic", topic, "payload", string(msg))
	c.mqtt.Publish(topic, 1 /*qos:at-least-once*/, true /*retain*/, msg)
	c.log.Info("Published:", "topic", topic, "payload", string(msg))
}
