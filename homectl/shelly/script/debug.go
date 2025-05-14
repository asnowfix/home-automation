package script

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"mynet"
	"net"
	"os"
	"os/signal"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"reflect"
	"strconv"
	"strings"
	"syscall"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

// Add cobra vommand to turn on script debugging using system.SetConfig

func init() {
	Cmd.AddCommand(debugCtl)
	debugCtl.Flags().IntVarP(&flags.Port, "port", "p", 0, "UDP port to listen on (default is to use dynamic port)")
}

var flags struct {
	Port int
}

var debugCtl = &cobra.Command{
	Use:   "debug",
	Short: "Turn on/off script debugging using system.SetConfig",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]

		var active bool
		if len(args) == 2 {
			active = args[1] == "true"
		}

		log := hlog.Logger

		// if configCmd.Flags().Changed("debug-mqtt") && flags.DebugMqtt != config.Debug.Mqtt.Enable {
		// 	config.Debug.Mqtt.Enable = flags.DebugMqtt
		// 	changed = true
		// }

		// if configCmd.Flags().Changed("debug-ws") && flags.DebugWebSocket != config.Debug.WebSocket.Enable {
		// 	config.Debug.WebSocket.Enable = flags.DebugWebSocket
		// 	changed = true
		// }

		// if configCmd.Flags().Changed("debug-udp") && flags.DebugUdp != "" && flags.DebugUdp != config.RpcUdp.DestinationAddress+":"+strconv.Itoa(int(config.RpcUdp.ListenPort)) {
		// 	config.RpcUdp.DestinationAddress = strings.Split(flags.DebugUdp, ":")[0]
		// 	port, err := strconv.Atoi(strings.Split(flags.DebugUdp, ":")[1])
		// 	if err != nil {
		// 		log.Error(err, "Invalid debug UDP address", "address", flags.DebugUdp)
		// 		return nil, err
		// 	}
		// 	config.RpcUdp.ListenPort = uint16(port)
		// 	changed = true
		// }

		var udpContext context.Context
		var udpCancel context.CancelFunc

		if active {
			port := flags.Port
			_, ip, err := mynet.MainInterface(hlog.Logger)
			if err != nil {
				return err
			}
			listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: *ip, Port: port})
			if err != nil {
				log.Error(err, "Unable to listen on UDP", "ip", ip.String(), "port", port)
				return err
			}
			defer listener.Close()

			// Get the actual port that was assigned (especially important if port was 0)
			localAddr := listener.LocalAddr().(*net.UDPAddr)
			port = localAddr.Port
			log.Info("Listening on UDP", "ip", ip.String(), "port", port)

			// Create a context for the UDP server only: others (MQTT ...etc) will timeout on their own
			udpContext, udpCancel = context.WithCancel(context.Background())
			defer udpCancel()
			udpChan := make(chan []byte)

			// Start a goroutine to read from UDP and send to channel
			go func(ctx context.Context, log logr.Logger, ch chan []byte) {
				for {
					buf := make([]byte, 1024)
					n, addr, err := listener.ReadFromUDP(buf)
					if err != nil {
						log.Error(err, "Unable to read from UDP", "ip", addr.String())
						continue
					}
					// log.Info("Received UDP message", "ip", addr.String(), "port", addr.Port, "bytes", n)

					// Make a copy of the data to avoid buffer reuse issues
					dataCopy := make([]byte, n)
					copy(dataCopy, buf[:n])

					// Send to channel or exit if context is done
					select {
					case ch <- dataCopy:
					case <-ctx.Done():
						return
					}
				}
			}(udpContext, log.WithName("UDP-logger"), udpChan)

			go func(ctx context.Context, log logr.Logger, ch chan []byte) {
				// Process messages from channel
				for {
					select {
					case <-ctx.Done():
						log.Info("Done")
						return

					case data := <-ch:
						parseMessage(log, data)
					}
				}
			}(udpContext, log, udpChan)

			addr := fmt.Sprintf("%s:%d", ip.String(), port)
			args = []string{addr}
		} else {
			args = []string{}
		}

		// FIXME: use udpContext
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doDebug, args)
		if err != nil {
			return err
		}

		if active {
			// Wait for ctrl-c signal to cancel the UDP context
			go func(log logr.Logger, cancel context.CancelFunc) {
				signals := make(chan os.Signal, 1)
				signal.Notify(signals, os.Interrupt)
				signal.Notify(signals, syscall.SIGTERM)
				<-signals
				log.Info("Received signal")
				cancel()
			}(log, udpCancel)
			<-udpContext.Done()
		}
		return nil
	},
}

func doDebug(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	var addr *string
	if len(args) > 0 {
		addr = &args[0]
	}
	config := system.Config{
		Debug: &system.DeviceDebug{
			Mqtt: system.Enabler{
				Enable: false,
			},
			WebSocket: system.Enabler{
				Enable: false,
			},
			Udp: system.EnablerUDP{
				Address: addr,
				Level:   4,
			},
		},
	}
	out, err := device.CallE(ctx, via, string(system.SetConfig), &system.SetConfigRequest{Config: config})
	if err != nil {
		log.Error(err, "Unable to turn script UDP debugging", "addr", addr)
		return nil, err
	}
	options.PrintResult(out)

	res, ok := out.(*system.SetConfigResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected format '%v' (failed to cast response)", reflect.TypeOf(out))
	}

	if res.RestartRequired {
		log.Info("Restart required")
		// TODO: restart device, if necessary
		// out, err := device.CallE(ctx, via, string(shelly.Reboot), nil)
		// if err != nil {
		// 	log.Error(err, "Unable to restart")
		// 	return nil, err
		// }
		// log.Info("Restarted", "out", out)
	}

	return out, nil
}

func parseMessage(log logr.Logger, data []byte) {
	// shelly1minig3-54320464074c 22008 84452.340 101 2|\"restart_required\": true, \"ts\": 1746952708.27999997138, \"cfg_rev\": 55 }\x00shelly1minig3-54320464074c 22009 84452.340 101 2|}\x00shelly1minig3-54320464074c 22010 84452.340 1 2|shelly_notification:211 Event from sys: {\"component\":\"sys\",\"event\":\"config_changed\",\"restart_required\":true,\"ts\":1746952708.28,\"cfg_rev\":55}\x00shelly1minig3-54320464074c 22011 84452.340 1 2|shelly_notification:165 Status change of sys: {\"cfg_rev\":55}\x00shelly1minig3-54320464074c 22012 84452.431 1 2|shos_rpc_inst.c:243     Wifi.GetStatus via MQTT
	// shelly1minig3-54320464074c 22061 84617.229 102 2|BLE scanner is listening to addresses: e8:e0:7e:a6:0c:6f
	// shelly1minig3-54320464074c 22062 84619.171 1 2|shos_rpc_inst.c:243     Shelly.GetDeviceInfo via MQTT
	// shelly1minig3-54320464074c 22063 84619.536 1 2|shos_rpc_inst.c:243     Shelly.ListMethods via MQTT
	// shelly1minig3-54320464074c 22064 84620.008 1 2|shos_rpc_inst.c:243     Script.Start via MQTT
	// shelly1minig3-54320464074c 22065 84620.009 1 2|shelly_user_script.:215 JS RAM stat: initial: 74768 after: 74740, used: 28
	// shelly1minig3-54320464074c 22066 84620.009 101 2|Now handling pool-house switch events
	// shelly1minig3-54320464074c 22067 84620.010 1 2|shelly_user_script.:231 JS RAM stat: after user code: 74768 after: 73656, used: 1112
	// shelly1minig3-54320464074c 22068 84620.024 1 2|shelly_notification:165 Status change of script:1: {\"id\":1,\"error_msg\":null,\"errors\":[],\"running\":true}
	// shelly1minig3-54320464074c 22069 84620.159 1 2|shos_rpc_inst.c:243     Wifi.GetStatus via MQTT
	// shelly1minig3-54320464074c 22070 84622.224 102 2|BLE scanner is listening to addresses: e8:e0:7e:a6:0c:6f" v=0
	// shelly1minig3-54320464074c 22071 84627.223 102 2|BLE scanner is listening to addresses: e8:e0:7e:a6:0c:6f" v=0
	// shelly1minig3-54320464074c 22072 84632.223 102 2|BLE scanner is listening to addresses: e8:e0:7e:a6:0c:6f" v=0

	// 0x00 is a line break
	lines := strings.Split(string(data), "\x00")

	// One each line:
	// <device> <message-count> <timestamp> <component> <lvl>|<message>
	for _, line := range lines {
		if line == "" {
			continue
		}

		// header is before "|", message is after
		entry := strings.SplitN(line, "|", 2)
		header := entry[0]

		fields := strings.Split(header, " ")
		if len(fields) != 5 {
			log.Error(nil, "Invalid line", "line", line)
			continue
		}

		device := fields[0]

		msgCount, err := strconv.Atoi(fields[1])
		if err != nil {
			log.Error(err, "Invalid msg count", "count", fields[1])
			continue
		}

		timestamp, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			log.Error(err, "Invalid timestamp", "timestamp", fields[2])
			continue
		}

		component, err := strconv.Atoi(fields[3])
		if err != nil {
			log.Error(err, "Invalid component", "component", fields[3])
			continue
		}

		// lvl, err := strconv.Atoi(fields[4])
		// if err != nil {
		// 	log.Error(err, "Invalid lvl", "lvl", fields[4])
		// 	continue
		// }

		if len(entry) != 2 {
			log.Error(nil, "Invalid line", "line", line)
			continue
		}
		msg := entry[1]

		// if <component> is 1xx, xx is the script number
		if component >= 100 && component < 200 {
			scriptNumber := component - 100
			log.Info("UDP-logger", "device", device, "count", msgCount, "ts", timestamp, "script", scriptNumber, "msg", msg)
		} else {
			log.Info("UDP-logger", "device", device, "count", msgCount, "ts", timestamp, "component", component, "msg", msg)
		}
	}
}
