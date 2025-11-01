package script

import (
	"fmt"
	"pkg/shelly/script"

	"github.com/spf13/cobra"
)

var runMinify bool

func init() {
	Cmd.AddCommand(runCmd)
	runCmd.Flags().BoolVar(&runMinify, "minify", false, "Minify script before running (default: false)")
}

var runCmd = &cobra.Command{
	Use:   "run <script-name>",
	Short: "Run a script locally (without uploading to a device)",
	Long: `Run a script locally using a JavaScript VM (goja) instead of uploading to a Shelly device.

This is useful for:
- Testing script syntax before uploading
- Debugging script logic locally
- Validating script changes

Note: The local execution environment differs from Shelly devices:
- Shelly-specific APIs (Shelly.call, MQTT, etc.) will not be available
- This only validates basic JavaScript syntax and execution
- Use this for quick validation, not as a replacement for device testing

Examples:
  # Run heater.js locally
  myhome ctl shelly script run heater.js
  
  # Run with minification (to test minifier)
  myhome ctl shelly script run heater.js --minify`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scriptName := args[0]
		
		// Run locally without device
		err := script.Run(cmd.Context(), scriptName, nil, runMinify)
		if err != nil {
			fmt.Printf("✗ Script execution failed: %v\n", err)
			return err
		}
		
		fmt.Printf("✓ Script %s executed successfully\n", scriptName)
		return nil
	},
}
