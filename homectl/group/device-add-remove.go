package group

import (
	"context"
	"myhome"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(deviceAddCmd)
	Cmd.AddCommand(deviceRemoveCmd)
}

var deviceAddCmd = &cobra.Command{
	Use:   "add-device",
	Short: "Add device to device group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		group := args[0]
		device := args[1]
		return deviceDo(cmd.Context(), myhome.GroupAddDevice, group, device)
	},
}

var deviceRemoveCmd = &cobra.Command{
	Use:   "remove-device",
	Short: "Remove device from device group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		group := args[0]
		device := args[1]
		return deviceDo(cmd.Context(), myhome.GroupRemoveDevice, group, device)
	},
}

func deviceDo(ctx context.Context, v myhome.Verb, group, device string) error {
	out, err := myhome.TheClient.CallE(ctx, myhome.DeviceLookup, device)
	if err != nil {
		return err
	}
	summary := out.(*myhome.DeviceSummary)
	_, err = myhome.TheClient.CallE(ctx, v, &myhome.GroupDevice{
		Group:        group,
		Manufacturer: summary.Manufacturer,
		Id:           summary.Id,
	})
	return err
}
