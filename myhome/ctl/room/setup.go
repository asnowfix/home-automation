package room

import (
	"encoding/json"
	"fmt"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/spf13/cobra"
)

var setupRoomCmd = &cobra.Command{
	Use:   "setup [room-id]",
	Short: "Push sensor topics and room config to heater devices in a room",
	Long: `Classifies devices in the given room (heater, temperature sensor, door/window sensor),
derives their MQTT topics, and writes the config to each heater's KVS.

If room-id is omitted, all rooms with assigned devices are set up.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		roomID := ""
		if len(args) == 1 {
			roomID = args[0]
		}

		result, err := myhome.TheClient.CallE(ctx, myhome.RoomSetup, &myhome.RoomSetupParams{RoomID: roomID})
		if err != nil {
			return fmt.Errorf("room.setup: %w", err)
		}

		r, ok := result.(*myhome.RoomSetupResult)
		if !ok {
			return fmt.Errorf("unexpected result type: %T", result)
		}

		data, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(data))
		return nil
	},
}

func init() {
	Cmd.AddCommand(setupRoomCmd)
}
