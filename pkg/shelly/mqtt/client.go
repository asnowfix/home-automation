package mqtt

import (
	"context"
)

type contextKey struct{}

// Message represents an MQTT message with topic and payload
type Message interface {
	Topic() string
	Payload() []byte
}

type Client interface {
	GetServer() string
	Id() string
	Subscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan []byte, error)
	Publisher(ctx context.Context, topic string, qlen uint, publisher string) (chan<- []byte, error)
	Publish(ctx context.Context, topic string, msg []byte) error
}

type Cache interface {
	Insert(topic string, msg []byte) error
}

// Subscriber is a capability interface for clients that support topic-aware subscriptions
// Note: This is intentionally NOT part of the base Client interface due to Go's lack of
// channel covariance. Implementations return channels of their own concrete Message types.
// Consumers should use type assertions to access this capability.
type MultiSubscriber interface {
	// MultiSubscriber returns a channel of messages that implement the Message interface
	// The actual channel type is implementation-specific
	MultiSubscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan Message, error)
}

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
