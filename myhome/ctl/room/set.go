package room

import (
	"fmt"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/spf13/cobra"
)

var setCmd = &cobra.Command{
	Use:   "set <device> <room-id>",
	Short: "Assign a device to a room",
	Long: `Assign a device to a room.

A device can belong to at most one room. Setting a new room will replace the previous assignment.
Use an empty string "" to remove the room assignment.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		roomId := args[1]

		client, err := myhome.ClientFromContext(cmd.Context())
		if err != nil {
			return err
		}

		params := &myhome.DeviceSetRoomParams{
			Identifier: device,
			RoomId:     roomId,
		}

		_, err = myhome.Call[*myhome.DeviceSetRoomParams, any](cmd.Context(), client, myhome.DeviceSetRoom, params)
		if err != nil {
			return err
		}

		if roomId == "" {
			fmt.Printf("Removed room assignment from device %s\n", device)
		} else {
			fmt.Printf("Assigned device %s to room %s\n", device, roomId)
		}
		return nil
	},
}
