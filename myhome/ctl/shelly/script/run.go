package script

import (
	"context"
	"fmt"
	"pkg/shelly/script"

	"github.com/spf13/cobra"
)

var runMinify bool
var runDeviceFile string

func init() {
	Cmd.AddCommand(runCmd)
	runCmd.Flags().BoolVar(&runMinify, "minify", false, "Minify script before running (default: false)")
	runCmd.Flags().StringVarP(&runDeviceFile, "device-file", "D", "device.json", "Device state file (KVS and Script.storage)")
}

var runCmd = &cobra.Command{
	Use:   "run <script-name>",
	Short: "Run a script locally (without uploading to a device)",
	Long: `Run a script locally using a JavaScript VM (goja) instead of uploading to a Shelly device.

This is useful for:
- Testing script syntax before uploading
- Debugging script logic locally
- Testing MQTT subscriptions and event handlers
- Validating script changes

The script will run until interrupted with Ctrl+C.

Note: The local execution environment provides:
- Shelly API placeholders (Shelly.call, MQTT.subscribe, etc.)
- Real MQTT connectivity for testing subscriptions
- Event loop for handling async operations
- Use --verbose flag to see detailed execution logs

Examples:
  # Run heater.js locally
  myhome ctl shelly script run heater.js
  
  # Run with verbose logging
  myhome ctl shelly script run heater.js --verbose
  
  # Run with minification (to test minifier)
  myhome ctl shelly script run heater.js --minify`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scriptName := args[0]

		fmt.Printf("Running script %s locally (press Ctrl+C to stop)...\n", scriptName)

		// Run locally without device, with device state file
		err := script.RunWithDeviceFile(cmd.Context(), scriptName, nil, runMinify, runDeviceFile)
		if err != nil && err != context.Canceled {
			fmt.Printf("\n✗ Script execution failed: %v\n", err)
			return err
		}

		fmt.Printf("\n✓ Script %s stopped\n", scriptName)
		return nil
	},
}
