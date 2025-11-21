package mqtt

import (
	"context"
	"fmt"
	"global"
	"myhome/ctl/options"
	"mynet"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	smqtt "pkg/shelly/mqtt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-logr/logr"
)

const HOSTNAME = "mqtt"
const BROKER_SERVICE = "_mqtt._tcp."
const PRIVATE_PORT = 1883
const PUBLIC_PORT = 8883

type Client struct {
	// clientId  string      // MQTT client_id (this client)
	mqtt                mqtt.Client        // MQTT stack
	brokerUrl           *url.URL           // MQTT broker to connect to
	log                 logr.Logger        // Logger to use
	resolutionTimeout   time.Duration      // MQTT broker (mDNS) lookup resolution timeout
	timeout             time.Duration      // MQTT operations timeout
	grace               time.Duration      // MQTT disconnection grace period
	watchdogStarted     bool               // Whether watchdog has been started
	watchdogMutex       sync.Mutex         // Protects watchdogStarted
	watchdogInterval    time.Duration      // Watchdog check interval
	watchdogMaxFailures int                // Watchdog max consecutive failures
	ctx                 context.Context    // Process-wide context for background services
	watchdogCancel      context.CancelFunc // Cancel function for watchdog
}

const BROKER_DEFAULT_NAME = "mqtt"

var mqttBroker string = BROKER_DEFAULT_NAME

var mqttOps *mqtt.ClientOptions

func init() {
	mqttOps = mqtt.NewClientOptions()
}

var client *Client

var mutex sync.Mutex

func GetClientE(ctx context.Context) (*Client, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	mutex.Lock()
	defer mutex.Unlock()

	if client != nil {
		return client, nil
	}

	mdnsCtx, mdnsCancel := context.WithTimeout(ctx, options.Flags.MdnsTimeout)
	defer mdnsCancel()
	brokerUrl, err := lookupBroker(mdnsCtx, log, mynet.MyResolver(log), mqttBroker)
	if err != nil {
		log.Error(err, "could not find MQTT broker", "where", mqttBroker)
		return nil, err
	}
	log.Info("Using MQTT broker", "url", brokerUrl)

	mqttOps.AddBroker(brokerUrl.String())
	mqttOps.Servers = []*url.URL{brokerUrl}

	client = &Client{
		// clientId:  clientId,
		mqtt:                mqtt.NewClient(mqttOps),
		brokerUrl:           brokerUrl,
		log:                 log,
		resolutionTimeout:   options.Flags.MdnsTimeout,
		timeout:             options.Flags.MqttTimeout,
		grace:               options.Flags.MqttGrace,
		watchdogInterval:    options.Flags.MqttWatchdogInterval,
		watchdogMaxFailures: options.Flags.MqttWatchdogMaxFailures,
		ctx:                 global.ProcessContext(ctx),
	}

	// FIXME: get MQTT logging right
	// 	Println: log.Info,
	// 	Printf:  log.I,
	// }

	log.Info("MQTT client initialized", "client_id", client.Id(), "timeout", client.timeout, "grace", client.grace)
	return client, nil
}

func NewClientE(ctx context.Context, broker string, mdnsTimeout time.Duration, mqttTimeout time.Duration, mqttGrace time.Duration) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}
	log = log.WithName("mqtt.Client")

	defer mutex.Unlock()
	mutex.Lock()

	if client != nil {
		return nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Error(err, "could not get hostname")
		return err
	}
	programName := os.Args[0]
	if i := strings.LastIndex(programName, string(os.PathSeparator)); i != -1 {
		programName = programName[i+1:]
	}
	clientId := fmt.Sprintf("%s-%s-%d", programName, hostname, os.Getpid())

	log.Info("Initializing MQTT client", "client_id", clientId, "timeout", mqttTimeout, "grace", mqttGrace)

	mqttOps.SetUsername(MqttUsername)
	mqttOps.SetPassword(MqttPassword)
	mqttOps.SetClientID(clientId)

	mqttOps.SetAutoReconnect(true) // automatically reconnect in case of disconnection
	mqttOps.SetResumeSubs(true)    // automatically re-subscribe in case or disconnection/reconnection
	mqttOps.SetCleanSession(false) // do not save messages to be re-sent in case of disconnection

	if broker != "" {
		mqttBroker = broker
	}

	mqttOps.SetConnectTimeout(mqttTimeout)
	return nil
}

func (c *Client) GetServer() string {
	// Host == <hostname>:<port>
	return c.brokerUrl.Host
}

func (c *Client) Id() string {
	opts := c.mqtt.OptionsReader()
	return opts.ClientID()
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

	// Start watchdog on first successful connection
	c.startWatchdogOnce()

	return nil
}

func (c *Client) startWatchdogOnce() {
	c.watchdogMutex.Lock()
	defer c.watchdogMutex.Unlock()

	if c.watchdogStarted {
		return
	}
	c.watchdogStarted = true

	// Create a cancellable context from the process context
	watchdogCtx, cancel := context.WithCancel(c.ctx)
	c.watchdogCancel = cancel

	go c.watchdog(watchdogCtx)
}

func (c *Client) watchdog(ctx context.Context) {
	consecutiveFailures := 0

	c.log.Info("Starting MQTT connection watchdog", "check_interval", c.watchdogInterval, "max_failures", c.watchdogMaxFailures)

	ticker := time.NewTicker(c.watchdogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Process terminating, stopping MQTT watchdog")
			return
		case <-ticker.C:
			if c.mqtt.IsConnected() {
				if consecutiveFailures > 0 {
					c.log.Info("MQTT connection recovered", "previous_failures", consecutiveFailures)
					consecutiveFailures = 0
				}
			} else {
				consecutiveFailures++
				c.log.Error(nil, "MQTT connection lost", "consecutive_failures", consecutiveFailures, "max_failures", c.watchdogMaxFailures)

				// Note: Paho MQTT client has AutoReconnect=true and ResumeSubs=true,
				// so it will automatically reconnect and re-subscribe to all topics.
				// We just monitor if reconnection is taking too long.

				if consecutiveFailures >= c.watchdogMaxFailures {
					c.log.Error(nil, "MQTT connection failed too many times, daemon needs restart",
						"consecutive_failures", consecutiveFailures)
					panic("MQTT connection permanently lost")
				}
			}
		}
	}
}

func lookupBroker(ctx context.Context, log logr.Logger, resolver mynet.Resolver, where string) (*url.URL, error) {
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

	log.Info("Looking up broker by IP", "addr", host)
	if ip := net.ParseIP(host); ip != nil {
		log.Info("Found IP", "addr", ip.String(), "port", port)
		return &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", ip.String(), port),
		}, nil
	}

	log.Info("Looking up broker by given host", "hostname", host)
	if ips, err := resolver.LookupHost(ctx, host); err == nil {
		ip := ips[0]
		log.Info("Found IP", "addr", ip.String(), "port", port)
		return &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", ip.String(), PRIVATE_PORT),
		}, nil
	}

	log.Info("Looking up broker by default host", "hostname", HOSTNAME)
	if ips, err := resolver.LookupHost(ctx, HOSTNAME); err == nil {
		ip := ips[0]
		log.Info("Found IP", "addr", ip.String(), "port", port)
		return &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", ip.String(), PRIVATE_PORT),
		}, nil
	}

	log.Info("Looking up broker by service", "service", BROKER_SERVICE)
	url, err := resolver.LookupService(ctx, BROKER_SERVICE)
	if err != nil {
		log.Error(err, "Service lookup failed", "service", BROKER_SERVICE)
		return nil, err
	}
	log.Info("Found service", "url", url)
	return url, nil
}

func (c *Client) Close() {
	c.log.Info("Closing MQTT client", "client_id", c.Id())

	// Cancel watchdog if it's running
	if c.watchdogCancel != nil {
		c.watchdogCancel()
	}

	if c.mqtt.IsConnected() {
		c.log.Info("Disconnecting MQTT client", "client_id", c.Id())
		c.mqtt.Disconnect(uint(c.grace.Milliseconds()))
		c.log.Info("Disconnected MQTT client", "client_id", c.Id())
	}
}

var MqttUsername string = ""

var MqttPassword string = ""

const ZEROCONF_SERVICE = "_mqtt._tcp."

type MqttMessage struct {
	topic   string
	payload []byte
}

// Topic returns the MQTT topic
func (m *MqttMessage) Topic() string {
	return m.topic
}

// Payload returns the MQTT message payload
func (m *MqttMessage) Payload() []byte {
	return m.payload
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

func (c *Client) Publisher(ctx context.Context, topic string, qlen uint) (chan<- []byte, error) {
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
				// Don't log context cancellation as an error
				return
			case msg, ok := <-mch:
				if !ok {
					log.Info("Channel closed", "topic", topic)
					return
				}
				c.Publish(ctx, topic, msg)
			}
		}
	}(c.log.WithName("Client#Publisher:" + topic))

	c.log.Info("Publisher running:", "topic", topic)
	return mch, nil
}

func (c *Client) Publish(ctx context.Context, topic string, msg []byte) error {
	err := c.connect()
	if err != nil {
		c.log.Error(err, "Unable to connect to publish", "topic", topic)
		return err
	}

	token := c.mqtt.Publish(topic, 1 /*qos:at-least-once*/, false /*retain*/, msg)
	if token.WaitTimeout(c.timeout) {
		// c.log.Info("Published", "to topic", topic, "payload", string(msg))
		return nil
	} else {
		c.log.Error(token.Error(), "Failed to publish", "to topic", topic, "payload", string(msg))
		return token.Error()
	}
}

func (c *Client) Subscriber(ctx context.Context, topic string, qlen uint) (<-chan []byte, error) {
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
		// Don't log context cancellation as an error
		token := c.mqtt.Unsubscribe(topic)
		if token.WaitTimeout(c.timeout) {
			log.Info("Unsubscribed", "from topic", topic)
		} else {
			log.Error(token.Error(), "Failed to unsubscribe", "from topic", topic)
		}
	}(ctx, c.log.WithName("Subscriber#Monitor"))

	return mch, nil
}

// SubscriberWithTopic returns a channel that receives MQTT messages with topic information
// This is useful for wildcard subscriptions where you need to know the actual topic
// Returns a channel of mqtt.Message interface - concrete type is *MqttMessage
func (c *Client) SubscriberWithTopic(ctx context.Context, topic string, qlen uint) (<-chan smqtt.Message, error) {
	err := c.connect()
	if err != nil {
		c.log.Error(err, "Unable to connect to create subscriber channel", "topic", topic)
		return nil, err
	}
	mch := make(chan smqtt.Message, qlen)

	token := c.mqtt.Subscribe(topic, 1 /*at-least-once*/, func(client mqtt.Client, msg mqtt.Message) {
		go func() {
			mch <- &MqttMessage{ // Send pointer to satisfy interface
				topic:   msg.Topic(),
				payload: msg.Payload(),
			}
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
		// Don't log context cancellation as an error
		token := c.mqtt.Unsubscribe(topic)
		if token.WaitTimeout(c.timeout) {
			log.Info("Unsubscribed", "from topic", topic)
		} else {
			log.Error(token.Error(), "Failed to unsubscribe", "from topic", topic)
		}
	}(ctx, c.log.WithName("SubscriberWithTopic#Monitor"))

	return (<-chan smqtt.Message)(mch), nil
}
