package gen2

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	mqttclient "github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/go-logr/logr"
)

type Listener struct {
	log     logr.Logger
	mqtt    mqttclient.Client
	service *events.Service
	tracker *events.SensorDailyTracker
}

func NewListener(log logr.Logger, mqtt mqttclient.Client, svc *events.Service, tracker *events.SensorDailyTracker) *Listener {
	return &Listener{
		log:     log.WithName("Gen2Listener"),
		mqtt:    mqtt,
		service: svc,
		tracker: tracker,
	}
}

func (l *Listener) Start(ctx context.Context) error {
	if err := l.mqtt.SubscribeWithHandler(ctx, "+/events/rpc", 16, "shelly/gen2/events", func(topic string, payload []byte, _ string) error {
		return l.handleRpc(ctx, payload)
	}); err != nil {
		l.log.Error(err, "Failed to subscribe to +/events/rpc")
		return err
	}

	if err := l.mqtt.SubscribeWithHandler(ctx, "+/online", 16, "shelly/gen2/online", func(topic string, payload []byte, _ string) error {
		return l.handleOnline(ctx, topic, payload)
	}); err != nil {
		l.log.Error(err, "Failed to subscribe to +/online")
		return err
	}

	l.log.Info("started")
	return nil
}

func (l *Listener) handleRpc(ctx context.Context, payload []byte) error {
	var hdr struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(payload, &hdr); err != nil {
		l.log.V(1).Info("Failed to parse +/events/rpc header", "error", err)
		return nil
	}
	switch hdr.Method {
	case "NotifyEvent":
		return l.handleNotifyEvent(ctx, payload)
	case "NotifyStatus":
		return l.handleNotifyStatus(ctx, payload)
	}
	return nil
}

func (l *Listener) handleNotifyStatus(ctx context.Context, payload []byte) error {
	var msg struct {
		Src    string                     `json:"src"`
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		l.log.V(1).Info("Failed to parse NotifyStatus", "error", err)
		return nil
	}

	for key, raw := range msg.Params {
		if key == "ts" {
			continue
		}
		// Only handle switch:N components for on/off events.
		if !strings.HasPrefix(key, "switch:") {
			continue
		}
		var sw struct {
			Output  *bool    `json:"output"`
			Ts      float64  `json:"ts"`
			APower  *float64 `json:"apower"`
			Voltage *float64 `json:"voltage"`
		}
		if err := json.Unmarshal(raw, &sw); err != nil || sw.Output == nil {
			continue
		}
		var ts float64
		if sw.Ts != 0 {
			ts = sw.Ts
		} else {
			ts = float64(time.Now().Unix())
		}
		eventName := "switch.off"
		if *sw.Output {
			eventName = "switch.on"
		}
		var dataPtr *string
		if sw.APower != nil || sw.Voltage != nil {
			m := make(map[string]float64, 2)
			if sw.APower != nil {
				m["apower"] = *sw.APower
			}
			if sw.Voltage != nil {
				m["voltage"] = *sw.Voltage
			}
			b, _ := json.Marshal(m)
			s := string(b)
			dataPtr = &s
		}
		e := events.Event{
			Ts:        ts,
			DeviceID:  msg.Src,
			Component: key,
			Event:     eventName,
			Severity:  "info",
			Data:      dataPtr,
		}
		if err := l.service.Record(ctx, e); err != nil {
			l.log.Error(err, "Failed to record switch status event", "device_id", msg.Src, "component", key)
		}
	}
	return nil
}

func (l *Listener) handleNotifyEvent(ctx context.Context, payload []byte) error {
	var msg struct {
		Src    string `json:"src"`
		Method string `json:"method"`
		Params struct {
			Ts     float64           `json:"ts"`
			Events []json.RawMessage `json:"events"`
		} `json:"params"`
	}

	if err := json.Unmarshal(payload, &msg); err != nil {
		l.log.V(1).Info("Failed to parse +/events/rpc message", "error", err)
		return nil
	}

	for _, raw := range msg.Params.Events {
		// Unmarshal known fields
		var entry struct {
			Component string  `json:"component"`
			ID        *int    `json:"id"`
			Event     string  `json:"event"`
			Ts        float64 `json:"ts"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			l.log.V(1).Info("Failed to parse event entry", "error", err)
			continue
		}

		// Unmarshal all fields to extract leftover data
		var all map[string]interface{}
		if err := json.Unmarshal(raw, &all); err != nil {
			l.log.V(1).Info("Failed to parse event entry as map", "error", err)
			continue
		}
		delete(all, "component")
		delete(all, "id")
		delete(all, "event")
		delete(all, "ts")

		var dataPtr *string
		if len(all) > 0 {
			b, _ := json.Marshal(all)
			s := string(b)
			dataPtr = &s
		}

		ts := entry.Ts
		if ts == 0 {
			if msg.Params.Ts >= 1e9 {
				// Floor to integer seconds: multiple gateways relaying the same
				// BLU broadcast within the same second share an identical ts and
				// are deduplicated by the UNIQUE(device_id,component,event,ts) constraint.
				ts = math.Floor(msg.Params.Ts)
			} else {
				ts = float64(time.Now().Unix())
			}
		}

		e := events.Event{
			Ts:        ts,
			DeviceID:  msg.Src,
			Component: entry.Component,
			Event:     entry.Event,
			Severity:  severityFor(entry.Event),
			Data:      dataPtr,
		}

		if err := l.service.Record(ctx, e); err != nil {
			l.log.Error(err, "Failed to record event", "device_id", msg.Src, "event", entry.Event)
		}

		// Feed tracker for sensor-change events
		switch entry.Event {
		case "illuminance.change":
			if v, ok := floatFrom(all, "lux"); ok {
				if err := l.tracker.Observe(ctx, events.Metric{DeviceID: msg.Src, Component: entry.Component, Metric: "lux"}, v); err != nil {
					l.log.Error(err, "tracker.Observe lux")
				}
			}
		case "temperature.change":
			if v, ok := floatFrom(all, "tC"); ok {
				if err := l.tracker.Observe(ctx, events.Metric{DeviceID: msg.Src, Component: entry.Component, Metric: "tC"}, v); err != nil {
					l.log.Error(err, "tracker.Observe tC")
				}
			}
		case "humidity.change":
			if v, ok := floatFrom(all, "rh"); ok {
				if err := l.tracker.Observe(ctx, events.Metric{DeviceID: msg.Src, Component: entry.Component, Metric: "rh"}, v); err != nil {
					l.log.Error(err, "tracker.Observe rh")
				}
			}
		}
	}

	return nil
}

func (l *Listener) handleOnline(ctx context.Context, topic string, payload []byte) error {
	// Topic is "<device_id>/online"; strip the "/online" suffix
	deviceID := strings.TrimSuffix(topic, "/online")
	if deviceID == topic {
		return nil
	}

	eventName := "device.offline"
	if string(payload) == "true" {
		eventName = "device.online"
	}

	now := float64(time.Now().Unix())
	e := events.Event{
		Ts:        now,
		DeviceID:  deviceID,
		Component: "mqtt",
		Event:     eventName,
		Severity:  "info",
	}

	if err := l.service.Record(ctx, e); err != nil {
		l.log.Error(err, "Failed to record connectivity event", "device_id", deviceID, "event", eventName)
	}
	return nil
}

func severityFor(event string) string {
	switch event {
	case "smoke.alarm", "smoke.alarm_test", "smoke.alarm_off":
		return "alarm"
	case "battery.low", "ota_error", "switch.active_power_change",
		"pool.fuse_tripped", "pool.water_supply_protected":
		return "warn"
	}
	if strings.HasPrefix(event, "input.button_") ||
		event == "temperature.change" ||
		event == "humidity.change" ||
		event == "ota_begin" ||
		event == "ota_progress" {
		return "debug"
	}
	return "info"
}

func floatFrom(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch f := v.(type) {
	case float64:
		return f, true
	case float32:
		return float64(f), true
	}
	return 0, false
}
