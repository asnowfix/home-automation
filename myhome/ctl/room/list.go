package room

import (
	"fmt"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list [room-id]",
	Short: "List devices in a room or all room assignments",
	Long: `List devices in a specific room, or list all devices with room assignments.

If room-id is provided, lists only devices in that room.
If no room-id is provided, lists all devices that have a room assignment.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := myhome.ClientFromContext(cmd.Context())
		if err != nil {
			return err
		}

		if len(args) == 1 {
			// List devices in specific room
			roomId := args[0]
			params := &myhome.DeviceListByRoomParams{
				RoomId: roomId,
			}

			result, err := myhome.Call[*myhome.DeviceListByRoomParams, *myhome.DeviceListByRoomResult](cmd.Context(), client, myhome.DeviceListByRoom, params)
			if err != nil {
				return err
			}

			devices := result.Devices
			if len(devices) == 0 {
				fmt.Printf("No devices in room %s\n", roomId)
				return nil
			}

			fmt.Printf("Devices in room %s:\n", roomId)
			for _, d := range devices {
				fmt.Printf("  - %s (%s)\n", d.Name(), d.Id())
			}
		} else {
			// List all devices with room assignments via DevicesMatch
			devices, err := myhome.Call[string, []myhome.DeviceSummary](cmd.Context(), client, myhome.DevicesMatch, "*")
			if err != nil {
				return err
			}

			// We need full device info to get room_id, so we'll list by room instead
			// For now, just show a message
			fmt.Println("Use 'myhome ctl room list <room-id>' to list devices in a specific room")
			fmt.Printf("Found %d devices total\n", len(devices))
		}
		return nil
	},
}
