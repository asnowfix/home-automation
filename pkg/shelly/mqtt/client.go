package mqtt

import (
	"context"
	"net/url"
	"sync"
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

var (
	client    Client
	clientMu  sync.RWMutex
	clientSet chan struct{}
)

func init() {
	clientSet = make(chan struct{})
}

func SetClient(c Client) {
	clientMu.Lock()
	defer clientMu.Unlock()
	if client != nil && c != client {
		panic("MQTT client already set with different value")
	}
	client = c
	close(clientSet)
}

func GetClient(ctx context.Context) Client {
	select {
	case <-clientSet:
		clientMu.RLock()
		defer clientMu.RUnlock()
		return client
	case <-ctx.Done():
		return nil
	}
}
