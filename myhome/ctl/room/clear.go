package room

import (
	"fmt"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/spf13/cobra"
)

var clearCmd = &cobra.Command{
	Use:   "clear <device>",
	Short: "Remove room assignment from a device",
	Long:  `Remove the room assignment from a device, leaving it unassigned.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]

		client, err := myhome.ClientFromContext(cmd.Context())
		if err != nil {
			return err
		}

		params := &myhome.DeviceSetRoomParams{
			Identifier: device,
			RoomId:     "", // Empty string clears the room
		}

		_, err = myhome.Call[*myhome.DeviceSetRoomParams, any](cmd.Context(), client, myhome.DeviceSetRoom, params)
		if err != nil {
			return err
		}

		fmt.Printf("Removed room assignment from device %s\n", device)
		return nil
	},
}
