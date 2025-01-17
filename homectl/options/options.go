package options

import "mymqtt"

var Flags struct {
	Json       bool
	Devices    string
	MqttBroker string
}

var Devices []string

var BrokerUrl string

var MqttClient *mymqtt.Client
