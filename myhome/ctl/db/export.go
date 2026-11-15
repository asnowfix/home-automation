package db

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/spf13/cobra"
)

var exportFlags struct {
	Output      string
	Pretty      bool
	DevicesOnly bool
}

var ExportCmd = &cobra.Command{
	Use:   "export [pattern]",
	Short: "Export database to JSON",
	Long: `Export the complete database to JSON format.

By default, exports devices (with their room assignment), rooms (with iCal URL), weekday defaults,
kind schedules, and the vacuum robots and their regions. Caches, the event log (events.db, a separate
file: copy it by hand if the target UI needs history or daily min/max) and device-registered fetch
subscriptions are not exported.
A section the daemon cannot serve (service disabled, or a daemon too old to know the method) is
reported as a warning on stderr, distinct from a section that is simply empty.
Use --devices-only to export only devices.

The output can be written to stdout (default) or to a file using --output.
Use a pattern to filter which devices to export (e.g., "shellyblu*").

To export from a specific myhome server instance, use the -I flag:
  myhome ctl -I myhome-local db export

Examples:
  # Export entire database to stdout
  myhome ctl db export

  # Export entire database to a file
  myhome ctl db export --output database.json

  # Export only devices matching a pattern
  myhome ctl db export "shellyblu*" --devices-only

  # Export with pretty formatting
  myhome ctl db export --pretty

  # Export from a specific myhome instance
  myhome ctl -I myhome-local db export --output local-database.json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		pattern := "*"
		if len(args) == 1 {
			pattern = args[0]
		}

		export, err := myhome.ExportDatabase(ctx, myhome.TheClient, pattern, exportFlags.DevicesOnly)
		if err != nil {
			return err
		}
		n := export.Count()
		if n == (myhome.ImportCounts{}) {
			for _, sk := range export.Skipped {
				fmt.Fprintf(os.Stderr, "⚠ %s\n", sk)
			}
			fmt.Fprintln(os.Stderr, "No data found to export")
			return nil
		}

		// Marshal to JSON
		var data []byte
		if exportFlags.Pretty {
			data, err = json.MarshalIndent(export, "", "  ")
		} else {
			data, err = json.Marshal(export)
		}
		if err != nil {
			return fmt.Errorf("failed to marshal database: %w", err)
		}

		// Write output
		if exportFlags.Output != "" {
			err = os.WriteFile(exportFlags.Output, data, 0644)
			if err != nil {
				return fmt.Errorf("failed to write to file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "✓ Exported to %s:\n", exportFlags.Output)
		} else {
			fmt.Println(string(data))
		}

		// Sections the daemon could not serve are not "0 items": say so.
		for _, sk := range export.Skipped {
			fmt.Fprintf(os.Stderr, "⚠ %s\n", sk)
		}

		// Print summary to stderr
		fmt.Fprintf(os.Stderr, "  - %d devices\n", n.Devices)
		if !exportFlags.DevicesOnly {
			fmt.Fprintf(os.Stderr, "  - %d temperature rooms\n", n.Rooms)
			fmt.Fprintf(os.Stderr, "  - %d weekday defaults\n", n.WeekdayDefaults)
			fmt.Fprintf(os.Stderr, "  - %d kind schedules\n", n.KindSchedules)
			fmt.Fprintf(os.Stderr, "  - %d vacuum robots, %d vacuum regions\n", n.VacuumRobots, n.VacuumRegions)
		}

		return nil
	},
}

func init() {
	ExportCmd.Flags().StringVarP(&exportFlags.Output, "output", "o", "", "Output file (default: stdout)")
	ExportCmd.Flags().BoolVar(&exportFlags.Pretty, "pretty", false, "Pretty-print JSON output")
	ExportCmd.Flags().BoolVar(&exportFlags.DevicesOnly, "devices-only", false, "Export only devices (skip temperature tables)")
}
