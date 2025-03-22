package shelly

import (
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
		shelly.Init(cmd.Context(), hopts.Flags.MqttTimeout)

		for i, c := range types.Channels {
			if options.Flags.Via == c {
				options.Via = types.Channel(i)
				break
			}
		}
	},
}

func init() {
	Cmd.PersistentFlags().StringVarP(&options.Flags.Via, "via", "V", types.ChannelDefault.String(), "Use given channel to communicate with Shelly devices (default is to discover it from the network)")

	Cmd.AddCommand(jobsCtl.Cmd)
	Cmd.AddCommand(mqttCtl.Cmd)
	Cmd.AddCommand(kvsCtl.Cmd)
	Cmd.AddCommand(scriptCtl.Cmd)
}
