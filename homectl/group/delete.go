package group

import (
	"fmt"
	"hlog"
	"myhome"

	"github.com/spf13/cobra"
)

var force bool

func init() {
	Cmd.AddCommand(deleteCmd)
	deleteCmd.Flags().BoolVarP(&force, "force", "f", false, "Force deletion of group")
}

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete device groups",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		log := hlog.Logger
		ctx := cmd.Context()

		if force {
			log.Info("Forcing deletion of group", "name", name)
			// get all devices of the group
			out, err := myhome.TheClient.CallE(ctx, myhome.GroupShow, name)
			if err != nil {
				return err
			}
			group, ok := out.(*myhome.Group)
			if !ok {
				return fmt.Errorf("expected myhome.Group, got %T", out)
			}
			log.Info("Removing from group", "name", name, "devices", len(group.Devices))
			for _, device := range group.Devices {
				_, err := myhome.TheClient.CallE(ctx, myhome.GroupRemoveDevice, &myhome.GroupDevice{
					Group:        name,
					Manufacturer: device.Manufacturer,
					Id:           device.Id,
				})
				if err != nil {
					return err
				}
			}
		}
		_, err := myhome.TheClient.CallE(ctx, myhome.GroupDelete, name)
		return err
	},
}
