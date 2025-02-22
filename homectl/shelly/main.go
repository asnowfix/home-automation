package shelly

import (
	"hlog"
	hopts "homectl/options"
	jobsCtl "homectl/shelly/jobs"
	kvsCtl "homectl/shelly/kvs"
	mqttCtl "homectl/shelly/mqtt"
	"homectl/shelly/options"
	scriptCtl "homectl/shelly/script"
	"pkg/shelly"
	"pkg/shelly/types"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "shelly",
	Short: "Shelly devices features",
	Args:  cobra.NoArgs,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		log := hlog.Logger
		log.Info("Init Shelly client API")
		shelly.Init(log, hopts.Flags.MqttTimeout)
		if options.Flags.ViaHttp {
			options.Via = types.ChannelHttp
		}
	},
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&options.Flags.ViaHttp, "via-http", "H", false, "Use HTTP channel to communicate with Shelly devices")

	Cmd.AddCommand(jobsCtl.Cmd)
	Cmd.AddCommand(mqttCtl.Cmd)
	Cmd.AddCommand(kvsCtl.Cmd)
	Cmd.AddCommand(scriptCtl.Cmd)
}
