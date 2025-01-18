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
	clientId  string      // MQTT client_id (this client)
	mqtt      mqtt.Client // MQTT stack
	brokerUrl *url.URL    // MQTT broker to connect to
	log       logr.Logger // Logger to use
}

func NewClient(log logr.Logger, broker string, me string) *Client {
	c, err := NewClientE(log, broker, me)
	if err != nil {
		panic(fmt.Errorf("could not initialize MQTT client: %w", err))
	}
	return c
}

func NewClientE(log logr.Logger, broker string, me string) (*Client, error) {
	if me == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("could not get hostname: %w", err)
		}

		me = fmt.Sprintf("%v-%v-%v", hostname, path.Base(os.Args[0]), os.Getpid())

	}
	log.Info("Initializing MQTT client", "client_id", me)

	opts := mqtt.NewClientOptions()
	opts.SetUsername(MqttUsername)
	opts.SetPassword(MqttPassword)
	opts.SetClientID(me)

	brokerUrl, err := lookupBroker(log, broker)
	if err != nil {
		log.Error(err, "could not find MQTT broker", "where", broker)
		return nil, err
	}
	log.Info("Using MQTT broker", "url", brokerUrl)

	opts.AddBroker(brokerUrl.String())
	opts.Servers = []*url.URL{brokerUrl}

	c := Client{
		clientId:  me,
		mqtt:      mqtt.NewClient(opts),
		log:       log,
		brokerUrl: brokerUrl,
	}
	c.log.Info("MQTT client initialized", "client_id", me)
	return &c, nil
}

func (c *Client) Id() string {
	return c.clientId
}

func (c *Client) BrokerUrl() *url.URL {
	return c.brokerUrl
}

func (c *Client) IsConnected() bool {
	return c.mqtt.IsConnected()
}

func (c *Client) connect() error {
	if c.mqtt.IsConnected() {
		return nil
	}

	token := c.mqtt.Connect()
	for !token.WaitTimeout(3 * time.Second) {
		c.log.Info("Trying to connect as MQTT client", "client_id", c.Id())
	}
	if err := token.Error(); err != nil {
		c.log.Error(err, "Failed to connect as MQTT client", "client_id", c.Id())
		return err
	}
	c.log.Info("Successfully connected as MQTT client", "client_id", c.Id())
	return nil
}

func lookupBroker(log logr.Logger, where string) (*url.URL, error) {
	log.Info("Looking up MQTT broker", "where", where)

	if where == "me" {
		log.Info("Finding local IP")
		_, ip, err := mynet.MainInterface(log)
		if err != nil {
			log.Error(err, "Could not get local IP")
			return nil, err
		}
		return &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", ip, PRIVATE_PORT),
		}, nil
	}

	p := strings.Split(where, ":")
	host := p[0]
	port := PRIVATE_PORT
	if len(p) > 1 {
		var err error
		port, err = strconv.Atoi(p[1])
		if err != nil {
			return nil, err
		}
	}

	if ip := net.ParseIP(host); ip != nil {
		log.Info("Using IP", "where", host, "port", port)
		return &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", host, port),
		}, nil
	}

	if _, err := net.LookupHost(host); err == nil {
		log.Info("Using host", "where", host, "port", port)
		return &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", where, PRIVATE_PORT),
		}, nil
	}

	url, err := lookupBrokerViaZeroConf(log)
	if err != nil {
		log.Error(err, "Zeroconf lookup failed", "service", BROKER_SERVICE)
		return nil, err
	}
	return url, nil
}

func (c *Client) Close() {
	if c.mqtt.IsConnected() {
		c.mqtt.Disconnect(250 /* milliseconds */)
	}
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
	c.connect()

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

func (c *Client) Publish(topic string, msg []byte) error {
	c.connect()
	c.log.Info("Publishing:", "topic", topic, "payload", string(msg))
	token := c.mqtt.Publish(topic, 1 /*qos:at-least-once*/, true /*retain*/, msg)
	token.Wait()
	c.log.Info("Published:", "topic", topic, "payload", string(msg))

	return nil
}

func (c *Client) Publisher(ctx context.Context, topic string, qlen uint) (chan<- []byte, error) {
	err := c.connect()
	if err != nil {
		return nil, err
	}
	mch := make(chan []byte, qlen)

	go func(log logr.Logger) {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-mch:
				log.Info("Publishing to MQTT:", "topic", topic, "payload", string(msg))
				token := c.mqtt.Publish(topic, 1 /*qos:at-least-once*/, true /*retain*/, msg)
				token.Wait()
				log.Info("Published to MQTT:", "topic", topic, "payload", string(msg))
			}
		}
	}(c.log.WithName("Client#Publisher"))

	c.log.Info("Publisher running:", "topic", topic)
	return mch, nil
}

func (c *Client) Subscriber(ctx context.Context, topic string, qlen uint) (<-chan []byte, error) {
	err := c.connect()
	if err != nil {
		return nil, err
	}
	mch := make(chan []byte, qlen)

	c.mqtt.Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
		c.log.Info("Received from MQTT:", "topic", topic, "payload", string(msg.Payload()))
		mch <- msg.Payload()
	})

	go func(ctx context.Context, log logr.Logger) {
		<-ctx.Done()
		log.Info("Cancelled")
		c.mqtt.Unsubscribe(topic)
		// select {
		// case <-ctx.Done():
		// 	log.Info("Cancelled")
		// 	c.mqtt.Unsubscribe(topic)
		// 	return
		// case <-mch:
		// 	log.Info("Closed by subscriber")
		// 	c.mqtt.Unsubscribe(topic)
		// }
	}(ctx, c.log.WithName("Client#Subscriber"))

	c.log.Info("Subscribed to:", "topic", topic)
	return mch, nil
}
