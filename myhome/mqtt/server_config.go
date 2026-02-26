package mqtt

import (
	"context"

	"github.com/go-logr/logr"
	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/spf13/viper"
)

// loadBrokerConfig loads broker configuration from Viper with defaults
// Supports loading from YAML/JSON config file using Viper's UnmarshalKey
func loadBrokerConfig(_ context.Context, log logr.Logger, v *viper.Viper) *mochimqtt.Options {
	// Start with default options
	config := &mochimqtt.Options{
		Capabilities: mochimqtt.NewDefaultServerCapabilities(),
	}

	// Configure aggressive session expiry and keepalive to detect stale clients quickly
	// This prevents leaked connections from accumulating when clients disconnect abruptly
	config.Capabilities.MaximumSessionExpiryInterval = 60 // Expire sessions after 60 seconds
	config.Capabilities.MaximumMessageExpiryInterval = 60 // Expire messages after 60 seconds
	config.Capabilities.MaximumClientWritesPending = 16   // Limit pending writes per client
	config.Capabilities.ReceiveMaximum = 1024             // Max concurrent QoS 1 and 2 messages
	config.Capabilities.MaximumPacketSize = 0             // No limit on packet size (0 = unlimited)

	if v != nil && v.IsSet("mqtt.broker") {
		// Unmarshal the entire mqtt.broker section into the Options struct
		// This automatically handles nested structures like Capabilities
		if err := v.UnmarshalKey("mqtt.broker", config); err != nil {
			log.Error(err, "Failed to unmarshal MQTT broker config, using defaults")
			return config
		}

		log.Info("MQTT broker configuration loaded from config file")
	} else {
		log.Info("No MQTT broker configuration found, using defaults")
	}

	log.V(1).Info("MQTT broker options",
		"capabilities", config.Capabilities,
		"client_net_write_buffer_size", config.ClientNetWriteBufferSize,
		"client_net_read_buffer_size", config.ClientNetReadBufferSize,
		"sys_topic_resend_interval", config.SysTopicResendInterval,
		"inline_client", config.InlineClient)

	return config
}
