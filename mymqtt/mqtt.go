package mymqtt

import (
	"context"
	"fmt"
	"hlog"
	"mynet"
	"net"
	"net/url"
	"os"
	"strconv"
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

type Client struct {
	clientId  string      // MQTT client_id (this client)
	mqtt      mqtt.Client // MQTT stack
	brokerUrl *url.URL    // MQTT broker to connect to
	log       logr.Logger // Logger to use
	timeout   time.Duration
	grace     time.Duration
}

var client *Client

var mutex sync.Mutex

func GetClientE(ctx context.Context) (*Client, error) {
	log := hlog.Logger
	mutex.Lock()
	for client == nil {
		mutex.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
		log.Info("Waiting for MQTT client to be initialized")
		time.Sleep(time.Second)
		mutex.Lock()
	}
	mutex.Unlock()
	return client, nil
}

func InitClient(ctx context.Context, log logr.Logger, broker string, me string, timeout time.Duration, grace time.Duration) *Client {
	c, err := InitClientE(ctx, log, broker, timeout, grace)
	if err != nil {
		panic(fmt.Errorf("could not initialize MQTT client: %w", err))
	}
	return c
}

func InitClientE(ctx context.Context, log logr.Logger, broker string, timeout time.Duration, grace time.Duration) (*Client, error) {
	defer mutex.Unlock()
	mutex.Lock()

	if client != nil {
		return client, nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Error(err, "could not get hostname")
		return nil, err
	}
	programName := os.Args[0]
	if i := strings.LastIndex(programName, "/"); i != -1 {
		programName = programName[i+1:]
	}
	clientId := fmt.Sprintf("%s-%s-%d", programName, hostname, os.Getpid())

	log.Info("Initializing MQTT client", "client_id", clientId, "timeout", timeout)

	opts := mqtt.NewClientOptions()
	opts.SetUsername(MqttUsername)
	opts.SetPassword(MqttPassword)
	opts.SetClientID(clientId)

	opts.SetAutoReconnect(true) // automatically reconnect in case of disconnection
	opts.SetResumeSubs(true)    // automatically re-subscribe in case or disconnection/reconnection
	opts.SetCleanSession(false) // do not save messages to be re-sent in case of disconnection

	brokerUrl, err := lookupBroker(log, broker)
	if err != nil {
		log.Error(err, "could not find MQTT broker", "where", broker)
		return nil, err
	}
	log.Info("Using MQTT broker", "url", brokerUrl)

	opts.AddBroker(brokerUrl.String())
	opts.Servers = []*url.URL{brokerUrl}

	client = &Client{
		clientId:  clientId,
		mqtt:      mqtt.NewClient(opts),
		brokerUrl: brokerUrl,
		log:       log,
		timeout:   timeout,
		grace:     grace,
	}

	// FIXME: get MQTT logging right
	// mqtt.DEBUG = mqtt.Logger{
	// 	Println: log.Info,
	// 	Printf:  log.I,
	// }

	go func(log logr.Logger) {
		<-ctx.Done()
		log.Error(ctx.Err(), "Context done: MQTT client disconnecting")
		client.Close()
	}(log.WithName("Client#Monitor"))

	log.Info("MQTT client initialized", "client_id", client.clientId, "timeout", client.timeout)
	return client, nil
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
	defer mutex.Unlock()
	mutex.Lock()
	if c.mqtt.IsConnected() {
		return nil
	}

	token := c.mqtt.Connect()
	for !token.WaitTimeout(c.timeout) {
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
	c.log.Info("Closing MQTT client", "client_id", c.Id())
	if c.mqtt.IsConnected() {
		c.log.Info("Disconnecting MQTT client", "client_id", c.Id())
		client.mqtt.Disconnect(uint(client.grace.Milliseconds()))
		c.log.Info("Disconnected MQTT client", "client_id", c.Id())
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
				log.Info("Discovered MQTT broker using mDNS", " ip", entry.AddrIPv4, "port", entry.Port)
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

// func (c *Client) Subscribe(topic string, qlen uint) (chan []byte, error) {
// 	err := c.connect()
// 	if err != nil {
// 		c.log.Error(err, "Unable to connect to subscribe to", "topic", topic)
// 		return nil, err
// 	}

// 	mch := make(chan []byte, qlen)

// 	c.log.Info("Subscribing to:", "topic", topic)
// 	token := c.mqtt.Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
// 		go func() {
// 			c.log.Info("Received from MQTT:", "topic", msg.Topic(), "payload", string(msg.Payload()))
// 			mch <- msg.Payload()
// 		}()
// 	})
// 	for !token.WaitTimeout(c.timeout) {
// 		c.log.Info("Trying to subscribe", "topic", topic, "as client_id", c.Id(), "timeout", c.timeout)
// 	}
// 	if err := token.Error(); err != nil {
// 		c.log.Error(token.Error(), "Subscription failed", "topic", topic, "as client_id", c.Id())
// 		return nil, err
// 	}
// 	c.log.Info("Subscribed", "topic", topic, "asclient_id", c.Id())
// 	return mch, nil
// }

// func (c *Client) Unsubscribe(topic string) {
// 	c.log.Info("Unsubscribing:", "topic", topic)
// 	token := c.mqtt.Unsubscribe(topic)
// 	if token.WaitTimeout(c.timeout) {
// 		c.log.Info("Unsubscribed:", "topic", topic)
// 	} else {
// 		c.log.Error(token.Error(), "Failed to unsubscribe from", "topic", topic)
// 	}
// }

func (c *Client) Publisher(ctx context.Context, topic string, qlen uint) (chan []byte, error) {
	err := c.connect()
	if err != nil {
		c.log.Error(err, "Unable to connect to create publisher channel", "topic", topic)
		return nil, err
	}
	mch := make(chan []byte, qlen)

	go func(log logr.Logger) {
		for {
			// log.Info("Waiting for message", "topic", topic)
			select {
			case <-ctx.Done():
				log.Error(ctx.Err(), "Context done", "topic", topic)
				return
			case msg, ok := <-mch:
				if !ok {
					log.Info("Channel closed", "topic", topic)
					return
				}
				c.Publish(topic, msg)
			}
		}
	}(c.log.WithName("Client#Publisher:" + topic))

	c.log.Info("Publisher running:", "topic", topic)
	return mch, nil
}

func (c *Client) Publish(topic string, msg []byte) error {
	token := c.mqtt.Publish(topic, 1 /*qos:at-least-once*/, false /*retain*/, msg)
	if token.WaitTimeout(c.timeout) {
		// c.log.Info("Published", "to topic", topic, "payload", string(msg))
		return nil
	} else {
		c.log.Error(token.Error(), "Failed to publish", "to topic", topic, "payload", string(msg))
		return token.Error()
	}
}

func (c *Client) Subscriber(ctx context.Context, topic string, qlen uint) (chan []byte, error) {
	err := c.connect()
	if err != nil {
		c.log.Error(err, "Unable to connect to create subscriber channel", "topic", topic)
		return nil, err
	}
	mch := make(chan []byte, qlen)

	token := c.mqtt.Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
		go func() {
			// c.log.Info("Received", "from topic", msg.Topic(), "payload", string(msg.Payload()))
			mch <- msg.Payload()
		}()
	})
	for !token.WaitTimeout(c.timeout) {
		c.log.Info("Retrying to subscribe", "to topic", topic, "as client_id", c.Id(), "timeout", c.timeout)
	}
	if err := token.Error(); err != nil {
		c.log.Error(token.Error(), "Subscription failed", "topic", topic, "as client_id", c.Id())
		return nil, err
	}
	c.log.Info("Subscribed", "to topic", topic, "as client_id", c.Id())

	go func(ctx context.Context, log logr.Logger) {
		<-ctx.Done()
		log.Error(ctx.Err(), "Context done", "topic", topic)
		token := c.mqtt.Unsubscribe(topic)
		if token.WaitTimeout(c.timeout) {
			log.Info("Unsubscribed", "from topic", topic)
		} else {
			log.Error(token.Error(), "Failed to unsubscribe", "from topic", topic)
		}
	}(ctx, c.log.WithName("Subscriber#Monitor"))

	return mch, nil
}
