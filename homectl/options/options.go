package options

import (
	"myhome"
	"mymqtt"
	"time"
)

var Flags struct {
	Verbose     bool
	ViaHttp     bool
	Json        bool
	Devices     string
	MqttBroker  string
	MqttTimeout time.Duration
}

var Devices []string

var MqttClient *mymqtt.Client

var MyHomeClient myhome.Client
