package options

import (
	"myhome"
	"mymqtt"
)

var Flags struct {
	Verbose    bool
	ViaHttp    bool
	Json       bool
	Devices    string
	MqttBroker string
}

var Devices []string

var BrokerUrl string

var MqttClient *mymqtt.Client

var MyHomeClient myhome.Client
