package show

import (
	"hlog"

	hopts "homectl/options"
	"homectl/shelly/options"

	"pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"
	"schedule"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var showAllFlag bool
var showCloudFlag bool
var showConfigFlag bool
var showMqttFlag bool
var showStatusFlag bool
var showSwitchId int
var showWifiFlag bool
var showJobsFlag bool

func init() {
	showShellyCmd.Flags().BoolVarP(&showAllFlag, "all", "a", false, "Show everything about (the) device(s).")
	showShellyCmd.Flags().BoolVarP(&showConfigFlag, "config", "c", false, "Show device configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showStatusFlag, "status", "s", false, "Show device Status(s).")
	showShellyCmd.Flags().IntVarP(&showSwitchId, "switch", "S", -1, "Show status of this switch ID.")
	showShellyCmd.Flags().BoolVarP(&showWifiFlag, "wifi", "W", false, "Show device Wifi configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showCloudFlag, "cloud", "C", false, "Show device Cloud configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showMqttFlag, "mqtt", "M", false, "Show device MQTT configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showJobsFlag, "jobs", "J", false, "Show device Scheduled Jobs configuration(s).")
}

var showShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Show Shelly devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		if showAllFlag {
			showCloudFlag = true
			showConfigFlag = true
			showMqttFlag = true
			showWifiFlag = true
			showJobsFlag = true
		}

		via := types.ChannelHttp
		if !options.UseHttpChannel {
			via = types.ChannelMqtt
		}

		shelly.Foreach(log, hopts.MqttClient, hopts.Devices, via, showOneDevice, args)
		return nil
	},
}

func showOneDevice(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {

	var s struct {
		DeviceInfo *shelly.DeviceInfo `json:"info"`
		Mqtt       struct {
			Config *mqtt.Config `json:"config,omitempty"`
			Status *mqtt.Status `json:"status,omitempty"`
		} `json:"mqtt,omitempty"`
		Switch struct {
			Config *sswitch.Config `json:"config,omitempty"`
			Status *sswitch.Status `json:"status,omitempty"`
		} `json:"switch,omitempty"`
		Scheduled *schedule.Scheduled `json:"scheduled,omitempty"`
	}

	s.DeviceInfo = device.Info
	// dc := shelly.CallMethod(device, "Shelly", "GetConfig").(*shelly.DeviceConfiguration)
	// ds := shelly.CallMethod(device, "Shelly", "GetStatus").(*shelly.DeviceStatus)

	if showMqttFlag {
		s.Mqtt.Config = device.Call(via, "Mqtt", "GetConfig", nil, &mqtt.ConfigResults{}).(*mqtt.Config)
		s.Mqtt.Status = device.Call(via, "Mqtt", "GetStatus", nil, &mqtt.Status{}).(*mqtt.Status)
	}

	if showSwitchId >= 0 {
		sr := make(map[string]interface{})
		sr["id"] = showSwitchId
		s.Switch.Config = device.Call(via, "Switch", "GetConfig", sr, &sswitch.Config{}).(*sswitch.Config)
		s.Switch.Status = device.Call(via, "Switch", "GetStatus", sr, &sswitch.Status{}).(*sswitch.Status)
	}

	if showJobsFlag {
		s.Scheduled = device.Call(via, "Schedule", "List", nil, &schedule.Scheduled{}).(*schedule.Scheduled)
	}

	return s, nil
}
