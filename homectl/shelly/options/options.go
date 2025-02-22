package options

import (
	"pkg/shelly/types"
)

var Flags struct {
	ViaHttp bool
	Devices string
}

var Via types.Channel = types.ChannelMqtt
