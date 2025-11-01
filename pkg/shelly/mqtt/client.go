package mqtt

import (
	"context"
)

type contextKey struct{}

type Client interface {
	GetServer() string
	Id() string
	Subscriber(ctx context.Context, topic string, qlen uint) (<-chan []byte, error)
	Publisher(ctx context.Context, topic string, qlen uint) (chan<- []byte, error)
	Publish(ctx context.Context, topic string, msg []byte) error
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
