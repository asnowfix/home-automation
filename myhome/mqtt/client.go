package mqtt

import (
	"context"
	"fmt"
	"global"
	"myhome/ctl/options"
	mynet "myhome/net"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-logr/logr"
)

const HOSTNAME = "mqtt"
const BROKER_SERVICE = "_mqtt._tcp."
const PRIVATE_PORT = 1883
const PUBLIC_PORT = 8883

type Message interface {
	Topic() string
	Payload() []byte
	Subscriber() string
}

type message struct {
	topic      string
	subscriber string
	payload    []byte
}

// Topic returns the MQTT topic
func (m *message) Topic() string {
	return m.topic
}

// Payload returns the MQTT message payload
func (m *message) Payload() []byte {
	return m.payload
}

// Payload returns the MQTT message named subscriber
func (m *message) Subscriber() string {
	return m.subscriber
}

// MQTT QoS levels
const (
	AtMostOnce  byte = 0 // QoS 0 - At most once delivery
	AtLeastOnce byte = 1 // QoS 1 - At least once delivery
	ExactlyOnce byte = 2 // QoS 2 - Exactly once delivery
)

// WaitForBrokerReady waits for an MQTT broker to be ready by attempting connections with exponential backoff
// This is useful when starting an embedded broker and needing to wait for it to be ready before connecting clients
func WaitForBrokerReady(ctx context.Context, log logr.Logger, brokerAddr string, initialDelay time.Duration, maxWait time.Duration) error {
	log.Info("Waiting for MQTT broker to be ready", "broker", brokerAddr, "initial_delay", initialDelay, "max_wait", maxWait)

	// Give the broker a moment to complete async initialization after Serve() returns
	// Mochi MQTT's Serve() is non-blocking and listeners start asynchronously
	if initialDelay > 0 {
		log.V(1).Info("Initial delay for broker async initialization", "delay", initialDelay)
		time.Sleep(initialDelay)
	}

	deadline := time.Now().Add(maxWait)
	attempt := 0
	backoff := 100 * time.Millisecond
	maxBackoff := 5 * time.Second

	for time.Now().Before(deadline) {
		attempt++

		log.V(1).Info("Attempting to connect to broker", "attempt", attempt, "broker", brokerAddr)

		// Create a dedicated test client (not using singleton) to avoid state issues
		opts := mqtt.NewClientOptions()
		opts.AddBroker(fmt.Sprintf("tcp://%s", brokerAddr))
		opts.SetClientID(fmt.Sprintf("readiness-check-%d", time.Now().UnixNano()))
		opts.SetConnectTimeout(1 * time.Second)
		opts.SetAutoReconnect(false)
		opts.SetCleanSession(true)
		opts.SetOrderMatters(false)

		testClient := mqtt.NewClient(opts)
		token := testClient.Connect()

		// Wait for connection with timeout
		if token.WaitTimeout(1 * time.Second) {
			if token.Error() == nil {
				// Successfully connected - broker is ready
				testClient.Disconnect(100)
				log.Info("MQTT broker is ready", "broker", brokerAddr, "attempts", attempt)
				return nil
			}
			log.V(1).Info("Connection failed", "attempt", attempt, "error", token.Error())
		} else {
			log.V(1).Info("Connection timed out", "attempt", attempt)
		}

		log.V(1).Info("Broker not ready yet, waiting", "attempt", attempt, "backoff", backoff)

		// Check if we've run out of time
		if time.Now().Add(backoff).After(deadline) {
			break
		}

		// Exponential backoff
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	return fmt.Errorf("MQTT broker at %s did not become ready within %v (after %d attempts)", brokerAddr, maxWait, attempt)
}

type Client interface {
	GetServer() string
	BrokerUrl() *url.URL
	Id() string
	Subscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan []byte, error)
	SubscribeWithHandler(ctx context.Context, topic string, qlen uint, subscriber string, handle func(topic string, payload []byte, subcriber string) error) error
	SubscribeWithTopic(ctx context.Context, topic string, qlen uint, subscriberName string) (<-chan Message, error)
	Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool, publisherName string) error
	Publisher(ctx context.Context, topic string, qlen uint, qos byte, retained bool, publisherName string) (chan<- []byte, error)
	IsConnected() bool
	Close()
}

type client struct {
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
	subscribers         sync.Map           // Map topic => []subscribers channels
}

const BROKER_DEFAULT_NAME = "mqtt"

var mqttBroker string = BROKER_DEFAULT_NAME

var mqttOps *mqtt.ClientOptions

func init() {
	mqttOps = mqtt.NewClientOptions()
}

var theClient *client

var mutex sync.Mutex

func GetClientE(ctx context.Context) (Client, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic(err)
	}

	mutex.Lock()
	defer mutex.Unlock()

	if theClient != nil {
		return theClient, nil
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

	theClient = &client{
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

	log.Info("MQTT client initialized", "client_id", theClient.Id(), "timeout", theClient.timeout, "grace", theClient.grace)

	return theClient, nil
}

func NewClientE(ctx context.Context, broker string, instanceName string, mdnsTimeout time.Duration, mqttTimeout time.Duration, mqttGrace time.Duration) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}
	log = log.WithName("mqtt.Client")

	defer mutex.Unlock()
	mutex.Lock()

	if theClient != nil {
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

	// Include instance name in client ID to ensure uniqueness across multiple daemon instances
	if instanceName == "" {
		instanceName = "default"
	}
	clientId := fmt.Sprintf("%s-%s-%s-%d", programName, instanceName, hostname, os.Getpid())

	log.Info("Initializing MQTT client", "client_id", clientId, "timeout", mqttTimeout, "grace", mqttGrace)

	mqttOps.SetUsername(MqttUsername)
	mqttOps.SetPassword(MqttPassword)
	mqttOps.SetClientID(clientId)

	mqttOps.SetAutoReconnect(true) // automatically reconnect in case of disconnection
	mqttOps.SetResumeSubs(true)    // automatically re-subscribe in case or disconnection/reconnection
	mqttOps.SetCleanSession(true)  // clean session to avoid stale subscriptions from previous instances
	mqttOps.SetOrderMatters(false) // required for wildcard subscriptions to route messages to correct handlers

	// DEBUG: default handler to catch messages not matched by any subscription route
	mqttOps.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		log.Info("DEFAULT HANDLER: unrouted message", "topic", msg.Topic(), "payload_len", len(msg.Payload()))
	})

	if broker != "" {
		mqttBroker = broker
	}

	mqttOps.SetConnectTimeout(mqttTimeout)
	return nil
}

func (c *client) GetServer() string {
	// Host == <hostname>:<port>
	return c.brokerUrl.Host
}

func (c *client) Id() string {
	opts := c.mqtt.OptionsReader()
	return opts.ClientID()
}

func (c *client) BrokerUrl() *url.URL {
	return c.brokerUrl
}

func (c *client) IsConnected() bool {
	return c.mqtt.IsConnected()
}

func (c *client) connect() error {
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

func (c *client) startWatchdogOnce() {
	c.watchdogMutex.Lock()
	defer c.watchdogMutex.Unlock()

	if c.watchdogStarted {
		return
	}
	c.watchdogStarted = true

	// Create a cancellable context from the process context
	watchdogCtx, cancel := context.WithCancel(c.ctx)
	c.watchdogCancel = cancel

	go c.watchdog(watchdogCtx, c.log.WithName("watchdog"))
}

func (c *client) watchdog(ctx context.Context, log logr.Logger) {
	consecutiveFailures := 0

	log.Info("Starting MQTT connection watchdog", "check_interval", c.watchdogInterval, "max_failures", c.watchdogMaxFailures)

	ticker := time.NewTicker(c.watchdogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Process terminating, stopping MQTT watchdog")
			return
		case <-ticker.C:
			if c.mqtt.IsConnected() {
				if consecutiveFailures > 0 {
					log.Info("MQTT connection recovered", "previous_failures", consecutiveFailures)
					consecutiveFailures = 0
				}
			} else {
				consecutiveFailures++
				log.Error(nil, "MQTT connection lost", "consecutive_failures", consecutiveFailures, "max_failures", c.watchdogMaxFailures)

				// Note: Paho MQTT client has AutoReconnect=true and ResumeSubs=true,
				// so it will automatically reconnect and re-subscribe to all topics.
				// We just monitor if reconnection is taking too long.

				if consecutiveFailures >= c.watchdogMaxFailures {
					log.Error(nil, "MQTT connection failed too many times, daemon needs restart",
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

func (c *client) Close() {
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

func (c *client) Publisher(ctx context.Context, topic string, qlen uint, qos byte, retained bool, publisherName string) (chan<- []byte, error) {
	err := c.connect()
	if err != nil {
		c.log.Error(err, "Unable to connect to create publisher channel", "topic", topic, "publisher", publisherName)
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
					log.Info("Channel closed", "topic", topic, "publisher", publisherName)
					return
				}
				c.Publish(ctx, topic, msg, qos, retained, publisherName)
			}
		}
	}(c.log.WithName(publisherName))

	c.log.Info("Publisher running:", "topic", topic)
	return mch, nil
}

func (c *client) Publish(ctx context.Context, topic string, msg []byte, qos byte, retained bool, publisherName string) error {
	err := c.connect()
	if err != nil {
		c.log.Error(err, "Unable to connect to publish", "topic", topic, "publisher", publisherName)
		return err
	}

	token := c.mqtt.Publish(topic, byte(qos), retained, msg)
	if token.WaitTimeout(c.timeout) {
		c.log.Info("Published", "to topic", topic, "payload", string(msg))
		return nil
	} else {
		c.log.Error(token.Error(), "Failed to publish", "to topic", topic, "payload", string(msg))
		return token.Error()
	}
}

func (c *client) Subscribe(ctx context.Context, topic string, qlen uint, subscriberName string) (<-chan []byte, error) {
	c.log.Info("Subscribe", "topic", topic, "qlen", qlen, "subscriber", subscriberName)
	return subscribe(c, ctx, topic, qlen, subscriberName, func(msg mqtt.Message) []byte {
		return msg.Payload()
	})
}

func (c *client) SubscribeWithTopic(ctx context.Context, topic string, qlen uint, subscriberName string) (<-chan Message, error) {
	c.log.Info("SubscribeWithTopic", "topic", topic, "qlen", qlen, "subscriber", subscriberName)
	return subscribe(c, ctx, topic, qlen, subscriberName, func(msg mqtt.Message) Message {
		return &message{
			subscriber: subscriberName,
			topic:      msg.Topic(),
			payload:    msg.Payload(),
		}
	})
}

func (c *client) SubscribeWithHandler(ctx context.Context, topic string, qlen uint, subscriberName string, handler func(topic string, payload []byte, subscriber string) error) error {
	c.log.Info("SubscribeWithHandler", "topic", topic, "qlen", qlen, "subscriber", subscriberName)
	mch, err := c.SubscribeWithTopic(ctx, topic, qlen, subscriberName)
	if err != nil {
		c.log.Error(err, "SubscribeWithHandler failed", "topic", topic, "subscriber", subscriberName)
		return err
	}
	c.log.Info("SubscribeWithHandler succeeded", "topic", topic, "subscriber", subscriberName)
	go func(log logr.Logger) {
		log.Info("Handler goroutine started", "topic", topic, "subscriber", subscriberName)
		for msg := range mch {
			log.V(1).Info("Handler received message from channel", "topic", msg.Topic(), "subscriber", msg.Subscriber(), "payload_len", len(msg.Payload()))
			handler(msg.Topic(), msg.Payload(), msg.Subscriber())
		}
		log.Info("Handler goroutine exiting", "topic", topic, "subscriber", subscriberName)
	}(c.log.WithName(subscriberName))
	return nil
}

func subscribe[T any](c *client, ctx context.Context, topic string, qlen uint, subscriberName string, transform func(mqtt.Message) T) (<-chan T, error) {
	err := c.connect()
	if err != nil {
		c.log.Error(err, "Unable to connect to create subscriber channel", "topic", topic)
		return nil, err
	}

	type subscriber struct {
		name  string
		queue chan T
	}

	// Create a channel for this subscriber
	s := &subscriber{
		name:  subscriberName,
		queue: make(chan T, qlen),
	}

	// Load or create subscriber list for this topic
	value, loaded := c.subscribers.LoadOrStore(topic, make([]*subscriber, 0))
	subscribers := value.([]*subscriber)
	subscribers = append(subscribers, s)
	c.subscribers.Store(topic, subscribers)
	c.log.Info("Subscriber added", "topic", topic, "subscriber", s.name, "count", len(subscribers), "qlen", qlen, "mqtt_subscribe_needed", !loaded)

	if !loaded {
		// Not yet subscribed at MQTT level: do it
		distribute := func(client mqtt.Client, msg mqtt.Message) {
			c.log.Info("distribute: message received from broker", "subscription_topic", topic, "message_topic", msg.Topic(), "payload_len", len(msg.Payload()))
			go func(log logr.Logger) {
				// Load current subscribers
				value, ok := c.subscribers.Load(topic)
				if !ok {
					log.Info("distribute: no subscribers found in map", "topic", topic)
					return
				}

				subscribers := value.([]*subscriber)
				dropping := make([]int, 0)

				// Transform and distribute to all subscribers
				transformed := transform(msg)
				log.V(1).Info("distribute: distributing to subscribers", "topic", topic, "subscriber_count", len(subscribers))
				for i, ch := range subscribers {
					select {
					case <-ctx.Done():
						c.log.Info("Subscriber context done", "topic", topic, "index", i, "error", ctx.Err())
						return
					case ch.queue <- transformed:
						log.V(1).Info("distribute: sent to subscriber", "topic", topic, "index", i, "subscriber", ch.name)
					default:
						// Channel full or closed, mark for removal
						c.log.Error(nil, "Subscriber channel full or closed", "topic", topic, "index", i, "subscriber", ch.name)
						dropping = append(dropping, i)
					}
				}

				// Remove dropped subscribers (iterate in reverse to maintain indices)
				if len(dropping) > 0 {
					for i := len(dropping) - 1; i >= 0; i-- {
						idx := dropping[i]
						err := fmt.Errorf("subscriber channel full or closed topic %s index %d subscriber %s", topic, idx, subscribers[idx].name)
						c.log.Error(err, "Dropping subscriber")
						close(subscribers[idx].queue)
						subscribers = append(subscribers[:idx], subscribers[idx+1:]...)
					}
					if len(subscribers) == 0 {
						// All subscribers dropped - delete topic entry so next Subscribe() will re-register at MQTT level
						c.log.Info("All subscribers dropped, resetting topic subscription state", "topic", topic)
						c.subscribers.Delete(topic)
					} else {
						c.subscribers.Store(topic, subscribers)
					}
				}
			}(c.log.WithName("distribute"))
		}

		token := c.mqtt.Subscribe(topic, 1 /*at-least-once*/, distribute)
		for !token.WaitTimeout(c.timeout) {
			c.log.Info("Retrying to subscribe", "to topic", topic, "as client_id", c.Id(), "timeout", c.timeout)
		}
		if err := token.Error(); err != nil {
			c.log.Error(token.Error(), "Subscription failed", "topic", topic, "as client_id", c.Id())
			return nil, err
		}
		c.log.Info("Subscribed", "to topic", topic, "as client_id", c.Id())
	}

	go func(log logr.Logger) {
		<-ctx.Done()
		// Don't log context cancellation as an error
		token := c.mqtt.Unsubscribe(topic)
		if token.WaitTimeout(c.timeout) {
			log.Info("Unsubscribed", "from topic", topic)
		} else {
			log.Error(token.Error(), "Failed to unsubscribe", "from topic", topic)
		}
	}(c.log.WithName(s.name))

	return s.queue, nil
}
