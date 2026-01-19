package db

import (
	"encoding/json"
	"fmt"
	"myhome"
	"os"

	"github.com/spf13/cobra"
)

var exportFlags struct {
	Output string
	Pretty bool
}

var ExportCmd = &cobra.Command{
	Use:   "export [pattern]",
	Short: "Export devices from the database to JSON",
	Long: `Export devices from the local database to JSON format.

The output can be written to stdout (default) or to a file using --output.
Use a pattern to filter which devices to export (e.g., "shellyblu*").

Examples:
  # Export all devices to stdout
  myhome ctl db export

  # Export all devices to a file
  myhome ctl db export --output devices.json

  # Export only BLU devices
  myhome ctl db export "shellyblu*" --output blu-devices.json

  # Export with pretty formatting
  myhome ctl db export --pretty`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pattern := "*"
		if len(args) == 1 {
			pattern = args[0]
		}

		// Get devices matching pattern
		devices, err := myhome.TheClient.LookupDevices(cmd.Context(), pattern)
		if err != nil {
			return fmt.Errorf("failed to lookup devices: %w", err)
		}

		if devices == nil || len(*devices) == 0 {
			fmt.Fprintln(os.Stderr, "No devices found matching pattern:", pattern)
			return nil
		}

		// Marshal to JSON
		var data []byte
		if exportFlags.Pretty {
			data, err = json.MarshalIndent(devices, "", "  ")
		} else {
			data, err = json.Marshal(devices)
		}
		if err != nil {
			return fmt.Errorf("failed to marshal devices: %w", err)
		}

		// Write output
		if exportFlags.Output != "" {
			err = os.WriteFile(exportFlags.Output, data, 0644)
			if err != nil {
				return fmt.Errorf("failed to write to file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "✓ Exported %d devices to %s\n", len(*devices), exportFlags.Output)
		} else {
			fmt.Println(string(data))
		}

		return nil
	},
}

func init() {
	ExportCmd.Flags().StringVarP(&exportFlags.Output, "output", "o", "", "Output file (default: stdout)")
	ExportCmd.Flags().BoolVar(&exportFlags.Pretty, "pretty", false, "Pretty-print JSON output")
}
