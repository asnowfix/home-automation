package mqtt

import (
	"runtime"
	"time"

	"github.com/mochi-mqtt/server/v2/packets"
)

func packetFor(topic string) packets.Packet { return packets.Packet{TopicName: topic} }

// runtimeGosched yields briefly between polls (no fixed sleep: callers re-check their condition).
func runtimeGosched() { runtime.Gosched(); time.Sleep(time.Millisecond) }
