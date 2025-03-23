package toggle

import (
	"context"
	"hlog"
	"homectl/options"
	"pkg/shelly"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var toggleSwitchId int

func init() {
	Cmd.Flags().IntVarP(&toggleSwitchId, "switch", "S", 0, "Use this switch ID.")
}

var Cmd = &cobra.Command{
	Use:   "toggle",
	Short: "Toggle switch devices",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := options.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, before, options.Via, toggleOneDevice, after)
	},
}

func toggleOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	sr := make(map[string]interface{})
	sr["id"] = toggleSwitchId
	out, err := device.CallE(ctx, via, string(sswitch.Toggle), sr)
	if err != nil {
		log.Info("Failed to toggle device %s: %v", device.Id_, err)
		return nil, err
	}
	return out, err
}
