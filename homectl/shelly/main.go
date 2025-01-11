package shelly

import (
	"hlog"
	jobsCtl "homectl/shelly/jobs"
	kvsCtl "homectl/shelly/kvs"
	mqttCtl "homectl/shelly/mqtt"
	"homectl/shelly/options"
	scriptCtl "homectl/shelly/script"
	"mymqtt"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "shelly",
	Short: "Shelly devices features",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		log := hlog.Init()
		var err error
		options.MqttClient, err = mymqtt.NewClientE(log, options.BrokerUrl)
		if err != nil {
			panic(err)
		}
	},
}

func init() {
	Cmd.PersistentFlags().StringVarP(&options.BrokerUrl, "mqtt-broker", "M", "", "Use given MQTT brpker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().BoolVarP(&options.UseHttpChannel, "http", "H", false, "Use HTTP channel to communicate with Shelly devices")
	Cmd.PersistentFlags().StringVarP(&options.DeviceNames, "devices", "d", "", "Shelly Device names to apply the command to")

	Cmd.AddCommand(jobsCtl.Cmd)
	Cmd.AddCommand(mqttCtl.Cmd)
	Cmd.AddCommand(kvsCtl.Cmd)
	Cmd.AddCommand(scriptCtl.Cmd)
}
