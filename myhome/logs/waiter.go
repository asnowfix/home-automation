package logs

import (
	"mymqtt"
	"net/url"

	"github.com/go-logr/logr"
)

func Waiter(log logr.Logger, broker *url.URL, tc chan string) {
	topics := make(map[string]chan mymqtt.MqttMessage)
	for topic := range tc {
		log.Info("logs.Waiter: topic: %v", topic)
		if _, exists := topics[topic]; !exists {
			log.Info("logs.Waiter: subscribing to %v", topic)
			tc, err := mymqtt.MqttSubscribe(log, broker, topic, 0 /*qlen*/)
			if err == nil {
				log.Info("logs.Waiter: subscribed to %v", topic)
				topics[topic] = tc
			} else {
				log.Info("logs.Waiter: error subscribing to %v (%v)", topic, err)
			}
			go func(t string, tc chan mymqtt.MqttMessage) {
				for msg := range tc {
					log.Info("logs.Waiter: %v: %v", t, msg.Payload)
				}
			}(topic, topics[topic])
		} else {
			log.Info("logs.Waiter: already known topic: %v", topic)
		}
	}
	for _, t := range topics {
		close(t)
	}
}
