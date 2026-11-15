package alert

import (
	"context"

	"github.com/asnowfix/home-automation/myhome/mqtt"
)

// Publisher sends a validated alert document to delivery.
type Publisher interface {
	Publish(ctx context.Context, a *Alert) error
}

type mqttPublisher struct {
	mc       mqtt.Client
	instance string
}

// NewMQTTPublisher publishes on <instance>/alert/<rule>/<key>, QoS 1, not retained.
// There is no outbox or republish: the broker is assumed reliable (#843).
func NewMQTTPublisher(mc mqtt.Client, instance string) Publisher {
	return &mqttPublisher{mc: mc, instance: instance}
}

func (p *mqttPublisher) Publish(ctx context.Context, a *Alert) error {
	payload, err := Encode(a) // validates
	if err != nil {
		return err
	}
	return p.mc.Publish(ctx, Topic(p.instance, a.Rule, a.Key), payload, mqtt.AtLeastOnce, false, "alert")
}
