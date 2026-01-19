package db

import (
	"encoding/json"
	"fmt"
	"io"
	"myhome"
	"os"

	"github.com/spf13/cobra"
)

var importFlags struct {
	Overwrite bool
}

var ImportCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import devices from JSON into the database",
	Long: `Import devices from a JSON file into the local database.

The input can be read from a file or from stdin.

Examples:
  # Import from a file
  myhome ctl db import devices.json

  # Import from stdin
  cat devices.json | myhome ctl db import

  # Import with overwrite (update existing devices)
  myhome ctl db import devices.json --overwrite`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var data []byte
		var err error

		if len(args) == 1 {
			// Read from file
			data, err = os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
		} else {
			// Read from stdin
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}
		}

		if len(data) == 0 {
			return fmt.Errorf("no data to import")
		}

		// Parse JSON
		var devices []myhome.Device
		err = json.Unmarshal(data, &devices)
		if err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}

		if len(devices) == 0 {
			fmt.Fprintln(os.Stderr, "No devices found in input")
			return nil
		}

		// Import each device
		imported := 0
		for _, device := range devices {
			_, err := myhome.TheClient.CallE(cmd.Context(), myhome.DeviceUpdate, &device)
			if err != nil {
				fmt.Fprintf(os.Stderr, "⚠ Failed to import %s: %v\n", device.Id(), err)
				continue
			}
			imported++
			fmt.Fprintf(os.Stderr, "✓ Imported %s (%s)\n", device.Name(), device.Id())
		}

		fmt.Fprintf(os.Stderr, "\n✓ Imported %d/%d devices\n", imported, len(devices))
		return nil
	},
}

func init() {
	ImportCmd.Flags().BoolVar(&importFlags.Overwrite, "overwrite", true, "Overwrite existing devices (default: true)")
}
