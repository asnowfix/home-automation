package logs

import (
	"devices"
	"log"
	"net/url"
)

func Waiter(broker *url.URL, tc chan string) {
	topics := make(map[string]chan devices.MqttMessage)
	for topic := range tc {
		log.Default().Printf("logs.Waiter: topic: %v", topic)
		if _, exists := topics[topic]; !exists {
			log.Default().Printf("logs.Waiter: subscribing to %v", topic)
			tc, err := devices.MqttSubscribe(broker, topic, 0 /*qlen*/)
			if err == nil {
				log.Default().Printf("logs.Waiter: subscribed to %v", topic)
				topics[topic] = tc
			} else {
				log.Default().Printf("logs.Waiter: error subscribing to %v (%v)", topic, err)
			}
			go func(t string, tc chan devices.MqttMessage) {
				for msg := range tc {
					log.Default().Printf("logs.Waiter: %v: %v", t, msg.Payload)
				}
			}(topic, topics[topic])
		} else {
			log.Default().Printf("logs.Waiter: already known topic: %v", topic)
		}
	}
	for _, t := range topics {
		close(t)
	}
}
