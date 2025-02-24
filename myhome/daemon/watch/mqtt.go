package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"global"
	"maps"
	"myhome"
	"myhome/devices"
	"mymqtt"
	"pkg/shelly"
	"pkg/shelly/mqtt"

	"github.com/go-logr/logr"
)

func Mqtt(ctx context.Context, mc *mymqtt.Client, dm devices.Manager, db devices.DeviceRegistry) error {
	log := ctx.Value(global.LogKey).(logr.Logger)

	var sd *shelly.Device

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
				log.Info("Received message", "topic", topic, "payload", string(msg))
				event := &mqtt.Event{}
				err := json.Unmarshal(msg, &event)
				if err != nil {
					log.Error(err, "Failed to unmarshal event from payload", "payload", string(msg))
					continue
				}
				if event.Src[:6] != "shelly" {
					log.Info("Skipping non-shelly event", "event", event)
					continue
				}
				deviceId := event.Src
				device, err := db.GetDeviceById(ctx, deviceId)
				if err != nil {
					log.Info("Device not found, creating new one", "device_id", deviceId)
					sd = shelly.NewDeviceFromMqttId(ctx, log, deviceId, mc)
					device, err = myhome.NewDeviceFromShellyDevice(ctx, log, sd)
					if err != nil {
						log.Error(err, "Failed to create device from shelly device")
						continue
					}
				}

				log.Info("Updating device", "device", device)
				err = UpdateFromMqttEvent(ctx, device, event)
				if err != nil {
					log.Error(err, "Failed to update device from MQTT event", "device_id", event.Src)
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
	if event.Method == "NotifyStatus" {
		if event.Params != nil {
			var err error
			status := make(map[string]interface{})
			if d.Status != nil {
				// FIXME: Convoluted way to merge status update map event in the current status
				out, err := json.Marshal(d.Status)
				if err != nil {
					log.Error(err, "failed to JSON-marshal current status")
					return err
				}
				err = json.Unmarshal(out, &status)
				if err != nil {
					log.Error(err, "failed to unmarshal current status")
					return err
				}
			}
			maps.Copy(status, *event.Params)
			out, err := json.Marshal(status)
			if err != nil {
				log.Error(err, "failed to JSON-(re)marshal updated status")
				return err
			}
			err = json.Unmarshal(out, &d.Status)
			if err != nil {
				log.Error(err, "failed to (re)unmarshal updated status")
				return err
			}
			// v := reflect.ValueOf(d.Status)
			// for i := 0; i < v.NumField(); i++ {
			// 	typeField := v.Type().Field(i)
			// 	valueField := v.Field(i)
			// 	log.Info("Updated status", "field", typeField.Name, "value", valueField.Interface())
			// }
		}
	}

	// - '{"src":"shellyplus1-08b61fd141e8","dst":"shellyplus1-08b61fd141e8/events","method":"NotifyFullStatus","params":{"ts":1736604018.38,"ble":{},"cloud":{"connected":true},"input:0":{"id":0,"state":false},"mqtt":{"connected":true},"switch:0":{"id":0, "source":"SHC", "output":true,"temperature":{"tC":48.4, "tF":119.2}},"sys":{"mac":"08B61FD141E8","restart_required":false,"time":"15:00","unixtime":1736604018,"uptime":658773,"ram_size":268520,"ram_free":110248,"fs_size":393216,"fs_free":106496,"cfg_rev":13,"kvs_rev":0,"schedule_rev":1,"webhook_rev":0,"available_updates":{"beta":{"version":"1.5.0-beta1"}},"reset_reason":3},"wifi":{"sta_ip":"192.168.1.76","status":"got ip","ssid":"Linksys_7A50","rssi":-58,"ap_client_count":0},"ws":{"connected":false}}}'
	if event.Method == "NotifyFullStatus" {
		out, err := json.Marshal(event.Params)
		if err != nil {
			log.Error(err, "failed to marshal updated full status")
			return err
		}
		err = json.Unmarshal(out, &d.Status)
		if err != nil {
			log.Error(err, "failed to unmarshal updated full status")
			return err
		}
	}

	// - '{"dst":"NCELRND1279_shellyplus1-08b61fd9333c","error":{"code":-109,"message":"shutting down in 952 ms"},"id":0,"result":{"methods":null},"src":"shellyplus1-08b61fd9333c"}'
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyEvent","params":{"ts":1736605194.11,"events":[{"component":"input:0","id":0,"event":"config_changed","restart_required":false,"ts":1736605194.11,"cfg_rev":35}]}}'
	if event.Method == "NotifyEvent" {
		if event.Params != nil {
			evs, ok := (*event.Params)["events"].([]mqtt.ComponentEvent)
			if ok {
				log.Info("Received event", "events", evs)
			} else {
				return fmt.Errorf("unable to parse event parameters: %v", event)
			}
		} else {
			return fmt.Errorf("missing event parameters in event: %v", event)
		}
	}

	return nil
}
