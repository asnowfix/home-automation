package logs

import (
	"mymqtt"
	"net/url"

	"github.com/go-logr/logr"
)

func Waiter(log logr.Logger, broker *url.URL, tc chan string) {
	topics := make(map[string]chan mymqtt.MqttMessage)
	for topic := range tc {
		log.Info("logs.Waiter:", "topic", topic)
		if _, exists := topics[topic]; !exists {
			log.Info("logs.Waiter: subscribing to", "topic", topic)
			tc, err := mymqtt.MqttSubscribe(log, broker, topic, 0 /*qlen*/)
			if err == nil {
				log.Info("logs.Waiter: subscribed to", "topic", topic)
				topics[topic] = tc
			} else {
				log.Error(err, "logs.Waiter: error subscribing to", "topic", topic)
			}
			go func(t string, tc chan mymqtt.MqttMessage) {
				for msg := range tc {
					log.Info("logs.Waiter", "topic", t, "payload", msg.Payload)
				}
			}(topic, topics[topic])
		} else {
			log.Info("logs.Waiter: already known", "topic", topic)
		}
	}
	for _, t := range topics {
		close(t)
	}
}
