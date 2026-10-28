package mqtt

import (
	"context"
	"net/url"
)

// MQTT QoS levels
const (
	AtMostOnce  byte = 0 // QoS 0 - At most once delivery
	AtLeastOnce byte = 1 // QoS 1 - At least once delivery
	ExactlyOnce byte = 2 // QoS 2 - Exactly once delivery
)

type Client interface {
	GetServer() string
	BrokerUrl() *url.URL
	Id() string
	Subscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan []byte, error)
	SubscribeWithHandler(ctx context.Context, topic string, qlen uint, subscriber string, handle func(topic string, payload []byte, subcriber string) error) error
	Publisher(ctx context.Context, topic string, qlen uint, qos byte, retained bool, publisherName string) (chan<- []byte, error)
	Publish(ctx context.Context, topic string, msg []byte, qos byte, retained bool, publisherName string) error
}

type Cache interface {
	Insert(topic string, msg []byte) error
}

// clientContextKey is the unexported type used to store a Client in a
// context.Context via NewContextWithClient/GetClient.
//
// This replaces the former SetClient/GetClient/ResetClient trio, a
// package-level mutable Client + sync.RWMutex + a "clientSet" channel that
// GetClient blocked on until SetClient closed it — plus a ResetClient
// escape hatch that existed purely so tests could reuse the package between
// runs, and a SetClient that panicked on a second call with a different
// value ("BUG: MQTT client already set with different value").
//
// Init(log, registrar, mc, timeout) is called exactly once per process, at
// a composition root (myhome/ctl.Cmd's PersistentPreRunE or
// myhome/daemon.daemon.Run), before any request-serving goroutine starts.
// The composition root wraps its own root ctx with the client right after
// calling Init and uses that wrapped ctx for everything downstream — the
// same pattern internal/myhome.NewContextWithClient uses for the RPC
// client (see #362) — so there is no start-up race between "client not set
// yet" and "first GetClient call" for GetClient to block on: by
// construction, the value is already present on any ctx a caller could
// have gotten from that root.
type clientContextKey struct{}

// NewContextWithClient returns a copy of ctx carrying c as the Shelly MQTT
// client. Called once, at a composition root, right after Init.
func NewContextWithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientContextKey{}, c)
}

// GetClient returns the Client stored by NewContextWithClient, or nil if
// ctx doesn't carry one (e.g. a composition root never ran Init).
func GetClient(ctx context.Context) Client {
	c, _ := ctx.Value(clientContextKey{}).(Client)
	return c
}
