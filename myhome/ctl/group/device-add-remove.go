package group

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"myhome/groups"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"reflect"

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
		dp := args[1]

		out, err := deviceDo(cmd.Context(), myhome.GroupAddDevice, group, dp, func(ctx context.Context, log logr.Logger, via types.Channel, gi *myhome.GroupInfo, device devices.Device) (*kvs.Status, error) {
			sd, ok := device.(*shelly.Device)
			if !ok {
				return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
			}
			for k, v := range gi.KeyValues() {
				log.Info("Adding", "key", k, "value", v)
				kvs.SetKeyValue(ctx, hlog.Logger, types.ChannelDefault, sd, k, v)
			}
			return kvs.SetKeyValue(ctx, hlog.Logger, types.ChannelDefault, sd, groups.KvsGroupPrefix+group, "true")
		})
		options.PrintResult(out)
		return err
	},
}

var deviceRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove device from group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		group := args[0]
		dp := args[1]

		out, err := deviceDo(cmd.Context(), myhome.GroupRemoveDevice, group, dp, func(ctx context.Context, log logr.Logger, via types.Channel, gi *myhome.GroupInfo, device devices.Device) (*kvs.Status, error) {
			sd, ok := device.(*shelly.Device)
			if !ok {
				return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
			}
			for k, v := range gi.KeyValues() {
				log.Info("Will NOT remove", "key", k, "value", v)
				// kvs.DeleteKey(ctx, hlog.Logger, types.ChannelDefault, device, k)
			}
			return kvs.DeleteKey(ctx, hlog.Logger, types.ChannelDefault, sd, groups.KvsGroupPrefix+group)
		})
		options.PrintResult(out)
		return err
	},
}

func deviceDo(ctx context.Context, v myhome.Verb, group, device string, fn func(ctx context.Context, log logr.Logger, via types.Channel, gi *myhome.GroupInfo, device devices.Device) (*kvs.Status, error)) (any, error) {
	out, err := myhome.TheClient.CallE(ctx, myhome.GroupShow, group)
	if err != nil {
		return nil, err
	}
	g, ok := out.(*myhome.Group)
	if !ok {
		return nil, fmt.Errorf("expected myhome.Group, got %T", out)
	}

	return myhome.Foreach(ctx, hlog.Logger, device, types.ChannelDefault, func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
		sd, err := shelly.NewDeviceFromSummary(ctx, log, device)
		if err != nil {
			log.Error(err, "Unable to create device from summary", "device", device)
			return nil, err
		}
		fn(ctx, log, types.ChannelDefault, &g.GroupInfo, sd)

		_, err = myhome.TheClient.CallE(ctx, v, &myhome.GroupDevice{
			Group:        group,
			Manufacturer: sd.Manufacturer(),
			Id:           sd.Id(),
		})
		return nil, err
	}, []string{})

}
