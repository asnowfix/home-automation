package roomba

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/go-logr/logr"
)

// Commands accepted on the robot's "cmd" topic.
const (
	CmdStart  = "start"
	CmdStop   = "stop"
	CmdPause  = "pause"
	CmdResume = "resume"
	CmdDock   = "dock"
	CmdFind   = "find" // plays a sound
)

// DefaultAckTimeout bounds the wait for a command's echo. Observed latency is 1-3 s.
const DefaultAckTimeout = 10 * time.Second

// ErrBadCredentials is returned by Connect when the robot rejects the BLID/password pair.
var ErrBadCredentials = errors.New("roomba: robot rejected the credentials")

// Client holds one long-lived MQTT connection to one robot.
//
// The robot refuses a connection that follows another one too closely (the
// retry 3-5 s later succeeds; seen in 6 of 6 runs), so a connection per command
// would be slow and flaky. Connect therefore retries, and the client is meant to
// stay up for the life of the daemon.
type Client struct {
	blid     string
	password string
	host     string
	log      logr.Logger
	onChange func(Status)

	mc      mqtt.Client
	publish func(topic string, payload []byte) error // replaced in tests

	mu    sync.Mutex
	state state
	acks  map[ackKey]chan struct{}
}

type ackKey struct {
	command string
	time    int64
}

// NewClient prepares a client; nothing happens on the network until Connect.
// onChange, if not nil, is called with the new Status after every pushed update
// once the first full state has arrived. It runs on the MQTT goroutine and must not block.
func NewClient(log logr.Logger, blid, password, host string, onChange func(Status)) *Client {
	c := &Client{
		blid:     blid,
		password: password,
		host:     host,
		log:      log.WithName("roomba").WithValues("blid", blid, "host", host),
		onChange: onChange,
		state:    state{},
		acks:     map[ackKey]chan struct{}{},
	}
	opts := mqtt.NewClientOptions().
		AddBroker("ssl://" + net.JoinHostPort(host, strconv.Itoa(MQTTPort))).
		SetClientID(blid).
		SetUsername(blid).
		SetPassword(password).
		SetCleanSession(false).
		SetProtocolVersion(4).
		SetTLSConfig(TLSConfig(blid)).
		SetConnectTimeout(10 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(time.Minute).
		SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) { c.handle(m.Payload()) }).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) { c.log.Info("Connection lost", "error", err) }).
		SetOnConnectHandler(func(mqtt.Client) { c.log.Info("Connected") })
	c.mc = mqtt.NewClient(opts)
	c.publish = func(topic string, payload []byte) error {
		t := c.mc.Publish(topic, 0, false, payload)
		if !t.WaitTimeout(5 * time.Second) {
			return fmt.Errorf("roomba: publish to %s timed out", topic)
		}
		return t.Error()
	}
	return c
}

// BLID returns the robot id this client talks to.
func (c *Client) BLID() string { return c.blid }

// Host returns the address this client connects to.
func (c *Client) Host() string { return c.host }

// Connect dials the robot, retrying every retry interval until it succeeds or ctx ends.
// A wrong password fails fast: retrying it cannot help.
func (c *Client) Connect(ctx context.Context, retry time.Duration) error {
	for attempt := 1; ; attempt++ {
		t := c.mc.Connect()
		select {
		case <-t.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
		err := t.Error()
		if err == nil {
			c.log.Info("Connect succeeded", "attempt", attempt)
			return nil
		}
		if errors.Is(err, packets.ErrorRefusedBadUsernameOrPassword) || errors.Is(err, packets.ErrorRefusedNotAuthorised) {
			return fmt.Errorf("%w: %v", ErrBadCredentials, err)
		}
		c.log.V(1).Info("Connect attempt failed", "attempt", attempt, "error", err)
		select {
		case <-time.After(retry):
		case <-ctx.Done():
			return fmt.Errorf("roomba: connect to %s: %w (last error: %v)", c.host, ctx.Err(), err)
		}
	}
}

// Connected reports whether the MQTT connection is currently up.
func (c *Client) Connected() bool { return c.mc.IsConnectionOpen() }

// Close disconnects.
func (c *Client) Close() { c.mc.Disconnect(250) }

// Status returns the latest merged state; ok is false until the first full state arrived.
func (c *Client) Status() (Status, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.ready() {
		return Status{}, false
	}
	return c.state.status(), true
}

// handle merges one pushed message, then releases matching command waiters and notifies onChange.
func (c *Client) handle(payload []byte) {
	c.mu.Lock()
	if !c.state.merge(payload) {
		c.mu.Unlock()
		return
	}
	lc := c.state.lastCommand()
	if ch, ok := c.acks[ackKey{lc.Command, lc.Time}]; ok {
		close(ch)
		delete(c.acks, ackKey{lc.Command, lc.Time})
	}
	var st Status
	ready := c.state.ready()
	if ready {
		st = c.state.status()
	}
	c.mu.Unlock()

	if ready && c.onChange != nil {
		c.onChange(st)
	}
}

// Command sends cmd with optional extra fields (e.g. a room clean's pmap_id and
// regions) and waits until the robot echoes it back in lastCommand, which is the
// only proof it was accepted. ctx bounds the wait; without a deadline,
// DefaultAckTimeout applies.
func (c *Client) Command(ctx context.Context, cmd string, extra map[string]any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultAckTimeout)
		defer cancel()
	}
	msg := map[string]any{}
	for k, v := range extra {
		msg[k] = v
	}
	key := ackKey{cmd, time.Now().Unix()}
	msg["command"], msg["time"], msg["initiator"] = key.command, key.time, "localApp"
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.mu.Lock()
	ch, pending := c.acks[key]
	if !pending {
		ch = make(chan struct{})
		c.acks[key] = ch
	}
	c.mu.Unlock()

	if err := c.publish("cmd", payload); err != nil {
		c.mu.Lock()
		delete(c.acks, key)
		c.mu.Unlock()
		return err
	}
	c.log.Info("Command sent", "command", cmd)

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.acks, key)
		c.mu.Unlock()
		return fmt.Errorf("roomba: %s not acknowledged by the robot: %w", cmd, ctx.Err())
	}
}

// CleanRegions starts a clean of the given regions of map m, in the given order.
func (c *Client) CleanRegions(ctx context.Context, m Map, regions []Region) error {
	if len(regions) == 0 {
		return errors.New("roomba: no region to clean")
	}
	return c.Command(ctx, CmdStart, map[string]any{
		"pmap_id":       m.ID,
		"user_pmapv_id": m.Version,
		"ordered":       1,
		"regions":       regions,
	})
}
