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

type contextKey struct{}

func FromContext(ctx context.Context) (Client, error) {
	out, ok := ctx.Value(contextKey{}).(Client)
	if !ok {
		panic("MQTT client not started")
		// return nil, fmt.Errorf("MQTT client not started")
	}
	return out, nil
}

func NewContext(ctx context.Context, client Client) context.Context {
	return context.WithValue(ctx, contextKey{}, client)
}
