package mqtt

import (
	"github.com/spf13/viper"
)

// BrokerConfig holds MQTT broker configuration
type BrokerConfig struct {
	MaximumSessionExpiryInterval uint32 // maximum number of seconds to keep disconnected sessions
	MaximumMessageExpiryInterval int64  // maximum message expiry if message expiry is 0 or over
	ReceiveMaximum               uint16 // maximum number of concurrent qos messages per client
	MaximumQos                   byte   // maximum qos value available to clients
	RetainAvailable              byte   // support of retain messages
	WildcardSubAvailable         byte   // support of wildcard subscriptions
	SubIDAvailable               byte   // support of subscription identifiers
	SharedSubAvailable           byte   // support of shared subscriptions
}

// DefaultBrokerConfig returns default broker configuration
func DefaultBrokerConfig() BrokerConfig {
	return BrokerConfig{
		MaximumSessionExpiryInterval: 300,          // 5 minutes - clean up disconnected sessions quickly
		MaximumMessageExpiryInterval: 60 * 60 * 24, // 24 hours for messages
		ReceiveMaximum:               65535,
		MaximumQos:                   2,
		RetainAvailable:              1,
		WildcardSubAvailable:         1,
		SubIDAvailable:               1,
		SharedSubAvailable:           1,
	}
}

// LoadBrokerConfig loads broker configuration from Viper with defaults
func LoadBrokerConfig(v *viper.Viper) BrokerConfig {
	cfg := DefaultBrokerConfig()

	if v == nil {
		return cfg
	}

	if v.IsSet("mqtt.broker.maximum_session_expiry_interval") {
		cfg.MaximumSessionExpiryInterval = v.GetUint32("mqtt.broker.maximum_session_expiry_interval")
	}
	if v.IsSet("mqtt.broker.maximum_message_expiry_interval") {
		cfg.MaximumMessageExpiryInterval = v.GetInt64("mqtt.broker.maximum_message_expiry_interval")
	}
	if v.IsSet("mqtt.broker.receive_maximum") {
		cfg.ReceiveMaximum = uint16(v.GetInt("mqtt.broker.receive_maximum"))
	}
	if v.IsSet("mqtt.broker.maximum_qos") {
		cfg.MaximumQos = byte(v.GetInt("mqtt.broker.maximum_qos"))
	}
	if v.IsSet("mqtt.broker.retain_available") {
		cfg.RetainAvailable = byte(v.GetInt("mqtt.broker.retain_available"))
	}
	if v.IsSet("mqtt.broker.wildcard_sub_available") {
		cfg.WildcardSubAvailable = byte(v.GetInt("mqtt.broker.wildcard_sub_available"))
	}
	if v.IsSet("mqtt.broker.sub_id_available") {
		cfg.SubIDAvailable = byte(v.GetInt("mqtt.broker.sub_id_available"))
	}
	if v.IsSet("mqtt.broker.shared_sub_available") {
		cfg.SharedSubAvailable = byte(v.GetInt("mqtt.broker.shared_sub_available"))
	}

	return cfg
}
