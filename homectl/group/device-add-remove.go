package group

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"net"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(deviceAddCmd)
	Cmd.AddCommand(deviceRemoveCmd)
}

var deviceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add device to group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		group := args[0]
		device := args[1]

		return deviceDo(cmd.Context(), myhome.GroupAddDevice, group, device, func(ctx context.Context, log logr.Logger, via types.Channel, gi *myhome.GroupInfo, device *shelly.Device) (*kvs.Status, error) {
			for k, v := range gi.KeyValues() {
				log.Info("Adding", "key", k, "value", v)
				kvs.SetKeyValue(ctx, hlog.Logger, types.ChannelDefault, device, k, v)
			}
			return kvs.SetKeyValue(ctx, hlog.Logger, types.ChannelDefault, device, fmt.Sprintf("group/%s", group), "true")
		})
	},
}

var deviceRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove device from group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		group := args[0]
		device := args[1]

		return deviceDo(cmd.Context(), myhome.GroupRemoveDevice, group, device, func(ctx context.Context, log logr.Logger, via types.Channel, gi *myhome.GroupInfo, device *shelly.Device) (*kvs.Status, error) {
			for k, v := range gi.KeyValues() {
				log.Info("Will NOT remove", "key", k, "value", v)
				// kvs.DeleteKey(ctx, hlog.Logger, types.ChannelDefault, device, k)
			}
			return kvs.DeleteKey(ctx, hlog.Logger, types.ChannelDefault, device, fmt.Sprintf("group/%s", group))
		})
	},
}

func deviceDo(ctx context.Context, v myhome.Verb, group, device string, fn func(ctx context.Context, log logr.Logger, via types.Channel, gi *myhome.GroupInfo, device *shelly.Device) (*kvs.Status, error)) error {
	log := hlog.Logger

	// get group info
	out, err := myhome.TheClient.CallE(ctx, myhome.GroupShow, group)
	if err != nil {
		return err
	}
	g, ok := out.(*myhome.Group)
	if !ok {
		return fmt.Errorf("expected myhome.Group, got %T", out)
	}

	// lookup devices
	out, err = myhome.TheClient.CallE(ctx, myhome.DeviceLookup, device)
	if err != nil {
		return err
	}
	devices, ok := out.(*[]myhome.DeviceSummary)
	if !ok {
		return fmt.Errorf("expected *[]myhome.DeviceSummary, got %T", out)
	}
	if len(*devices) != 1 {
		return fmt.Errorf("expected 1 device, got %d", len(*devices))
	}
	summary := (*devices)[0]

	fn(ctx, log, types.ChannelDefault, &g.GroupInfo, shelly.NewDeviceFromIp(ctx, log, net.ParseIP(summary.Host)))

	_, err = myhome.TheClient.CallE(ctx, v, &myhome.GroupDevice{
		Group:        group,
		Manufacturer: summary.Manufacturer,
		Id:           summary.Id(),
	})
	return err
}
