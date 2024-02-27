package logs

import (
	"devices"
	"log"
)

func Waiter(tc chan string) {
	topics := make(map[string]chan devices.MqttMessage)
	for topic := range tc {
		log.Default().Printf("logs.Waiter: topic: %v", topic)
		if _, exists := topics[topic]; !exists {
			log.Default().Printf("logs.Waiter: subscribing to topic: %v", topic)
			tc, err := devices.MqttSubscribe("logs.Waiter", topic, 0 /*qlen*/)
			if err == nil {
				log.Default().Printf("subscribed to %v", topic)
				topics[topic] = tc
			} else {
				log.Default().Printf("error subscribing to %v: %v", topic, err)
			}
			go func(t string, tc chan devices.MqttMessage) {
				for msg := range tc {
					log.Default().Printf("logs.Waiter: %v >>> %v", t, msg)
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
