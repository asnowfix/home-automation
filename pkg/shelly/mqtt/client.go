package mqtt

import (
	"context"
)

type contextKey struct{}

type Subscriber func(topic string, payload []byte) error

type Client interface {
	GetServer() string
	Id() string
	Subscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan []byte, error)
	SubscribeAll(ctx context.Context, topic string, qlen uint, subscriber string, handle Subscriber) error
	Publisher(ctx context.Context, topic string, qlen uint, publisher string) (chan<- []byte, error)
	Publish(ctx context.Context, topic string, msg []byte) error
}

type Cache interface {
	Insert(topic string, msg []byte) error
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
