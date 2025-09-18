package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"global"
	"homectl/options"
	"myhome"
	"myhome/devices"
	"mymqtt"
	"os"
	"path/filepath"
	shellyapi "pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/shelly"
	"time"

	"github.com/go-logr/logr"
)

func Mqtt(ctx context.Context, mc *mymqtt.Client, dm devices.Manager, db devices.DeviceRegistry) error {
	log := ctx.Value(global.LogKey).(logr.Logger)

	topic := "+/events/rpc"
	ch, err := mc.Subscriber(ctx, topic, 16)
	if err != nil {
		log.Error(err, "Failed to subscribe to shelly devices events")
		return err
	}

	go func(ctx context.Context, log logr.Logger) error {
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled", "topic", topic)
				return ctx.Err()

			case msg := <-ch:
				log.Info("Received RPC event", "topic", topic, "payload", string(msg))
				if len(msg) < 2 {
					log.Info("Skipping RPC event with invalid payload", "payload", string(msg))
					continue
				}

				// If an events directory is configured, persist raw payload as a JSON file
				if dir := options.Flags.EventsDir; dir != "" {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						log.Error(err, "Failed to create events directory", "dir", dir)
					} else {
						// Use RFC3339 timestamp for filename
						ts := time.Now().UTC().Format(time.RFC3339)
						filename := filepath.Join(dir, fmt.Sprintf("%s.json", ts))
						if werr := os.WriteFile(filename, msg, 0o644); werr != nil {
							log.Error(werr, "Failed to write event payload", "file", filename)
						} else {
							log.Info("Wrote event", "file", filename)
						}
					}
				}

				event := &mqtt.Event{}
				err := json.Unmarshal(msg, &event)
				if err != nil {
					log.Error(err, "Failed to unmarshal RPC event from payload", "payload", string(msg))
					continue
				}
				if event.Src[:6] != "shelly" {
					log.Info("Skipping non-shelly RPC event", "event", event)
					continue
				}
				deviceId := event.Src
				device, err := db.GetDeviceById(ctx, deviceId)
				if err != nil {
					log.Info("Device not found from DB, creating new one", "device_id", deviceId)
					sd, err := shellyapi.NewDeviceFromMqttId(ctx, log, deviceId)
					if err != nil {
						log.Error(err, "Failed to create device from shelly device", "device_id", deviceId)
						continue
					}
					device, err = myhome.NewDeviceFromImpl(ctx, log, sd)
					if err != nil {
						log.Error(err, "Failed to create device from shelly device", "device_id", deviceId)
						continue
					}
				} else {
					log.Info("Found device in DB", "device", device.DeviceSummary)
					if device.Impl() == nil {
						log.Info("Loading device details in memory", "device", device.DeviceSummary)
						sd, err := shellyapi.NewDeviceFromSummary(ctx, log, device)
						if err != nil {
							log.Error(err, "Failed to create device from summary", "device", device.DeviceSummary)
							continue
						}
						device = device.WithImpl(sd)
					}
				}

				log.Info("Updating device from MQTT event", "device", device.DeviceSummary)
				err = UpdateFromMqttEvent(ctx, device, event)
				if err != nil {
					log.Error(err, "Failed to update device from MQTT event", "src", event.Src, "device", device.DeviceSummary)
					continue
				}

				dm.UpdateChannel() <- device
			}
		}
	}(ctx, log.WithName("Mqtt#Watcher"))

	return nil
}

func UpdateFromMqttEvent(ctx context.Context, d *myhome.Device, event *mqtt.Event) error {
	log := ctx.Value(global.LogKey).(logr.Logger)

	// Events like:
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyStatus","params":{"ts":1736603810.49,"switch:0":{"id":0,"output":false,"source":"HTTP_in"}}}'
	// - '{"src":"shellyplus1-08b61fd90730","dst":"shellyplus1-08b61fd90730/events","method":"NotifyStatus","params":{"ts":1736604020.06,"cloud":{"connected":true}}}'
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyStatus","params":{"ts":1736605194.11,"sys":{"cfg_rev":35}}}'
	//
	// - '{"src":"shellyplus1-08b61fd141e8","dst":"shellyplus1-08b61fd141e8/events","method":"NotifyFullStatus","params":{"ts":1736604018.38,"ble":{},"cloud":{"connected":true},"input:0":{"id":0,"state":false},"mqtt":{"connected":true},"switch:0":{"id":0, "source":"SHC", "output":true,"temperature":{"tC":48.4, "tF":119.2}},"sys":{"mac":"08B61FD141E8","restart_required":false,"time":"15:00","unixtime":1736604018,"uptime":658773,"ram_size":268520,"ram_free":110248,"fs_size":393216,"fs_free":106496,"cfg_rev":13,"kvs_rev":0,"schedule_rev":1,"webhook_rev":0,"available_updates":{"beta":{"version":"1.5.0-beta1"}},"reset_reason":3},"wifi":{"sta_ip":"192.168.1.76","status":"got ip","ssid":"Linksys_7A50","rssi":-58,"ap_client_count":0},"ws":{"connected":false}}}'
	if event.Method == "NotifyStatus" || event.Method == "NotifyFullStatus" {
		if event.Params != nil {
			statusStr, err := json.Marshal(event.Params)
			if err != nil {
				log.Error(err, "Failed to (re-)marshal status", "event", event)
				return err
			}
			status := &shelly.Status{}
			if err := json.Unmarshal(statusStr, status); err != nil {
				log.Error(err, "Failed to unmarshal status", "event", event)
				return err
			}
			d.Update(status)
		}
	}

	// - '{"dst":"NCELRND1279_shellyplus1-08b61fd9333c","error":{"code":-109,"message":"shutting down in 952 ms"},"id":0,"result":{"methods":null},"src":"shellyplus1-08b61fd9333c"}'
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyEvent","params":{"ts":1736605194.11,"events":[{"component":"input:0","id":0,"event":"config_changed","restart_required":false,"ts":1736605194.11,"cfg_rev":35}]}}'
	// - '{"src":"shellypro3-34987a48c26c","dst":"shellypro3-34987a48c26c/events","method":"NotifyEvent","params":{"ts":1758144175.35,"events":[{"component":"sys","event":"sys_btn_down","ts":1758144175.35}]}}
	// - '{"src":"shellypro3-34987a48c26c","dst":"shellypro3-34987a48c26c/events","method":"NotifyEvent","params":{"ts":1758144175.54,"events":[{"component":"sys","event":"sys_btn_up","ts":1758144175.54}]}}'
	// - '{"src":"shellypro3-34987a48c26c","dst":"shellypro3-34987a48c26c/events","method":"NotifyEvent","params":{"ts":1758144175.54,"events":[{"component":"sys","event":"sys_btn_push","ts":1758144175.54}]}}'
	if event.Method == "NotifyEvent" {
		if event.Params != nil {
			evs, ok := (*event.Params)["events"].([]mqtt.ComponentEvent)
			if ok {
				for _, ev := range evs {
					log.Info("Event", "component", ev.Component, "event", ev.Event)
					if ev.ConfigRevision != nil {
						d.ConfigRevision = *ev.ConfigRevision
					}
					if ev.RestartRequired != nil {
						log.Info("Event", "component", ev.Component, "event", ev.Event, "restart_required", *ev.RestartRequired)
					}
				}
			} else {
				return fmt.Errorf("unable to parse event parameters: %v", *event)
			}
		} else {
			return fmt.Errorf("missing event parameters in event: %v", event)
		}
	}

	return nil
}
