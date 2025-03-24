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
	Use:   "add-device",
	Short: "Add device to group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		group := args[0]
		device := args[1]

		return deviceDo(cmd.Context(), myhome.GroupAddDevice, func(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (*kvs.Status, error) {
			return kvs.SetKeyValue(ctx, hlog.Logger, types.ChannelDefault, device, fmt.Sprintf("group/%s", group), "true")
		}, group, device)
	},
}

var deviceRemoveCmd = &cobra.Command{
	Use:   "remove-device",
	Short: "Remove device from group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		group := args[0]
		device := args[1]

		return deviceDo(cmd.Context(), myhome.GroupRemoveDevice, func(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (*kvs.Status, error) {
			return kvs.DeleteKey(ctx, hlog.Logger, types.ChannelDefault, device, fmt.Sprintf("group/%s", group))
		}, group, device)
	},
}

func deviceDo(ctx context.Context, v myhome.Verb, fn func(context.Context, logr.Logger, types.Channel, *shelly.Device, []string) (*kvs.Status, error), group, device string) error {
	log := hlog.Logger
	out, err := myhome.TheClient.CallE(ctx, myhome.DeviceLookup, device)
	if err != nil {
		return err
	}
	devices, ok := out.(*myhome.Devices)
	if !ok {
		return fmt.Errorf("expected myhome.Devices, got %T", out)
	}
	if len(devices.Devices) != 1 {
		return fmt.Errorf("expected 1 device, got %d", len(devices.Devices))
	}
	summary := devices.Devices[0]

	fn(ctx, log, types.ChannelDefault, shelly.NewDeviceFromIp(ctx, log, net.ParseIP(summary.Host)), []string{group})

	_, err = myhome.TheClient.CallE(ctx, v, &myhome.GroupDevice{
		Group:        group,
		Manufacturer: summary.Manufacturer,
		Id:           summary.Id,
	})
	return err
}
