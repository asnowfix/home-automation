package mqtt

import (
	"context"
	"math"
	"strings"

	"github.com/go-logr/logr"
	"github.com/go-viper/mapstructure/v2"
	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/spf13/viper"
)

// loadBrokerConfig loads broker configuration from Viper with defaults
// Supports loading from YAML/JSON config file using Viper's UnmarshalKey.
//
// writesPending is the value of the dedicated setting (--mqtt-broker-client-writes-pending /
// daemon.mqtt_broker_client_writes_pending). It is passed in rather than read from viper because
// the flag, env var and daemon.* key are already merged into options.Flags by the daemon's PreRunE
// (flag > env/config file > default), and this package must not import the options package.
// A value > 0 wins over capabilities.maximum_client_writes_pending of the mqtt.broker block;
// 0 leaves Mochi's default (or the mqtt.broker block value, if set); a negative value is ignored
// with a warning (the CLI rejects it earlier).
func loadBrokerConfig(_ context.Context, log logr.Logger, v *viper.Viper, writesPending int) *mochimqtt.Options {
	// Start with default options
	config := &mochimqtt.Options{
		Capabilities: mochimqtt.NewDefaultServerCapabilities(),
	}

	// Configure aggressive session expiry and keepalive to detect stale clients quickly
	// This prevents leaked connections from accumulating when clients disconnect abruptly
	config.Capabilities.MaximumSessionExpiryInterval = 60 // Expire sessions after 60 seconds
	config.Capabilities.MaximumMessageExpiryInterval = 60 // Expire messages after 60 seconds
	config.Capabilities.ReceiveMaximum = 1024             // Max concurrent QoS 1 and 2 messages
	config.Capabilities.MaximumPacketSize = 0             // No limit on packet size (0 = unlimited)

	if v != nil && v.IsSet("mqtt.broker") {
		// Unmarshal the entire mqtt.broker section into the Options struct
		// This automatically handles nested structures like Capabilities.
		// Keys match Go field names ignoring case and underscores, so both the idiomatic snake_case
		// spelling (maximum_client_writes_pending) and the legacy one (maximumclientwritespending)
		// work. Keys that match no field are collected and reported (issue #865).
		var md mapstructure.Metadata
		err := v.UnmarshalKey("mqtt.broker", config, func(dc *mapstructure.DecoderConfig) {
			dc.MatchName = matchBrokerKey
			dc.Metadata = &md
		})
		if err != nil {
			log.Error(err, "Failed to unmarshal MQTT broker config, using defaults")
			return config
		}
		for _, key := range md.Unused {
			log.Info("WARNING: ignoring unknown key in mqtt.broker config (typo? snake_case of a Mochi Options field is expected)", "key", "mqtt.broker."+key)
		}

		log.Info("MQTT broker configuration loaded from config file")
	} else {
		log.Info("No MQTT broker configuration found, using defaults")
	}

	// Per-client outbound queue depth. The queue is allocated per client up front (one pointer per
	// slot), and a full queue silently drops the message (see dropLogger), so it must never be
	// tiny, zero or negative (make(chan, negative) panics inside Mochi).
	defaultPending := mochimqtt.NewDefaultServerCapabilities().MaximumClientWritesPending
	switch {
	case writesPending < 0:
		log.Info("Ignoring negative MQTT broker client writes pending", "value", writesPending)
	case writesPending > 0:
		config.Capabilities.MaximumClientWritesPending = int32(min(writesPending, math.MaxInt32))
	}
	if config.Capabilities.MaximumClientWritesPending <= 0 {
		config.Capabilities.MaximumClientWritesPending = defaultPending
	}

	log.V(1).Info("MQTT broker options",
		"capabilities", config.Capabilities,
		"client_net_write_buffer_size", config.ClientNetWriteBufferSize,
		"client_net_read_buffer_size", config.ClientNetReadBufferSize,
		"sys_topic_resend_interval", config.SysTopicResendInterval,
		"inline_client", config.InlineClient)

	return config
}

// matchBrokerKey reports whether a config key designates a struct field, ignoring case and
// underscores: "maximum_client_writes_pending" and "MaximumClientWritesPending" are the same.
func matchBrokerKey(mapKey, fieldName string) bool {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }
	return norm(mapKey) == norm(fieldName)
}
