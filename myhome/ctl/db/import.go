package db

import (
	"fmt"
	"io"
	"os"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/spf13/cobra"
)

var importFlags struct {
	Overwrite   bool
	DevicesOnly bool
}

var ImportCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import database from JSON",
	Long: `Import database from a JSON file.

Supports the unified export format (any section may be absent, so files written by older
versions import unchanged) and the legacy devices-only format.
The input can be read from a file or from stdin.

To import to a specific myhome server instance, use the -I flag:
  myhome ctl -I myhome-local db import database.json

Examples:
  # Import entire database from a file
  myhome ctl db import database.json

  # Import from stdin
  cat database.json | myhome ctl db import

  # Import only devices (skip temperature tables)
  myhome ctl db import database.json --devices-only

  # Import to a specific myhome instance (e.g., local dev server)
  myhome ctl -I myhome-local db import database.json

  # Export from default instance and import to local instance
  myhome ctl db export --output database.json
  myhome ctl -I myhome-local db import database.json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
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

		dbImport, err := myhome.ParseDatabaseImport(data)
		if err != nil {
			return err
		}
		n, err := myhome.ImportDatabase(ctx, myhome.TheClient, dbImport, importFlags.DevicesOnly, os.Stderr)
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "\n✓ Import complete:\n")
		fmt.Fprintf(os.Stderr, "  - %d devices\n", n.Devices)
		if !importFlags.DevicesOnly {
			fmt.Fprintf(os.Stderr, "  - %d temperature rooms\n", n.Rooms)
			fmt.Fprintf(os.Stderr, "  - %d weekday defaults\n", n.WeekdayDefaults)
			fmt.Fprintf(os.Stderr, "  - %d kind schedules\n", n.KindSchedules)
			fmt.Fprintf(os.Stderr, "  - %d vacuum robots, %d vacuum regions\n", n.VacuumRobots, n.VacuumRegions)
		}

		return nil
	},
}

func init() {
	ImportCmd.Flags().BoolVar(&importFlags.Overwrite, "overwrite", true, "Overwrite existing entries (default: true)")
	ImportCmd.Flags().BoolVar(&importFlags.DevicesOnly, "devices-only", false, "Import only devices (skip temperature tables)")
}
