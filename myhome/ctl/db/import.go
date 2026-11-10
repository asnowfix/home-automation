package db

import (
	"fmt"
	"github.com/asnowfix/home-automation/internal/myhome"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var importFlags struct {
	Overwrite     bool
	DevicesOnly   bool
	CreateMissing bool
}

var ImportCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import database from JSON",
	Long: `Import database from a JSON file.

Supports both the unified export format (devices + temperature tables) and legacy devices-only format.
The input can be read from a file or from stdin.

Rooms, weekday defaults and kind schedules are overwritten when they differ. Devices are NOT
recreated unless --create-missing is given (the database keeps no record of forgotten devices,
so an absent device cannot be told from a forgotten one); a device the target already holds only
gets its room assignment restored, and the room-id KVS key is then re-written on the physical
device (a device write; unreachable devices are listed, the database is still updated).
A section the target daemon does not serve is reported as NOT IMPORTED and the command exits non-zero.

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

		dump, err := myhome.ParseDatabaseDump(data)
		if err != nil {
			return err
		}

		if importFlags.CreateMissing {
			fmt.Fprintln(os.Stderr, "! --create-missing: devices absent from the target database will be created")
		}
		fmt.Fprintln(os.Stderr, "! Room assignments written to devices also WRITE the room-id KVS key on the physical device")

		report, err := myhome.ImportDatabase(ctx, myhome.TheClient, dump, myhome.ImportOptions{
			DevicesOnly:   importFlags.DevicesOnly,
			CreateMissing: importFlags.CreateMissing,
		})
		printReport(report)
		if err != nil {
			return fmt.Errorf("import interrupted: %w", err)
		}
		if report.Incomplete() {
			return fmt.Errorf("import incomplete: see the report above")
		}
		return nil
	},
}

func init() {
	ImportCmd.Flags().BoolVar(&importFlags.Overwrite, "overwrite", true, "Overwrite existing entries (default: true)")
	ImportCmd.Flags().BoolVar(&importFlags.CreateMissing, "create-missing", false, "Also create devices absent from the target database (default: only restore room assignments of known devices, so forgotten devices are not resurrected)")
	ImportCmd.Flags().BoolVar(&importFlags.DevicesOnly, "devices-only", false, "Import only devices (skip temperature tables)")
}

func printSection(name string, s myhome.SectionReport) {
	switch {
	case s.NotRequested:
		return
	case s.Unavailable != "":
		fmt.Fprintf(os.Stderr, "  - %s: NOT IMPORTED, %s\n", name, s.Unavailable)
		return
	}
	fmt.Fprintf(os.Stderr, "  - %s: %d created, %d updated, %d unchanged, %d skipped, %d failed\n",
		name, s.Created, s.Updated, s.Unchanged, s.Skipped, s.Failed)
	for _, n := range s.Notes {
		fmt.Fprintf(os.Stderr, "      %s\n", n)
	}
}

func printReport(r *myhome.ImportReport) {
	fmt.Fprintf(os.Stderr, "\nImport report:\n")
	printSection("devices", r.Devices)
	printSection("rooms", r.Rooms)
	printSection("weekday defaults", r.WeekdayDefaults)
	printSection("kind schedules", r.KindSchedules)
	k := r.RoomKVS
	if k.Set+k.NotApplicable+k.Unknown+len(k.Unreachable) > 0 {
		fmt.Fprintf(os.Stderr, "  - room-id KVS written on devices: %d set, %d n/a (Gen1/BLU), %d unknown (older daemon), %d unreachable\n",
			k.Set, k.NotApplicable, k.Unknown, len(k.Unreachable))
		for _, u := range k.Unreachable {
			fmt.Fprintf(os.Stderr, "      unreachable (database updated, device not): %s\n", u)
		}
	}
}
