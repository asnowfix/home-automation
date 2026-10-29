// Package vacuum implements `myhome ctl vacuum`: robot vacuum control through
// the daemon's vacuum.* RPC methods (#606). The one exception is `pair`, which
// calls pkg/roomba directly so the robot's password never crosses RPC (#626).
// No robot protocol code lives here.
package vacuum

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/roomba"
	"github.com/spf13/cobra"
)

var robotFlag string

// Cmd is `myhome ctl vacuum`.
var Cmd = &cobra.Command{
	Use:   "vacuum",
	Short: "Control robot vacuums (iRobot Roomba, local LAN protocol)",
	Long: `Control robot vacuums through the myhome daemon, which talks to each robot
directly on the LAN. The iRobot cloud account (MYHOME_IROBOT_*) is used only to
read the robot's rooms ('vacuum rooms sync').

--robot selects a robot by name or id; it can be omitted (the daemon has one
configured robot, see MYHOME_VACUUM_* in docs/configuration.md).`,
	Args: cobra.NoArgs,
}

func init() {
	Cmd.PersistentFlags().StringVarP(&robotFlag, "robot", "r", "", "robot name or id (default: the configured robot)")
	Cmd.AddCommand(listCmd, statusCmd, cleanCmd, roomsCmd, pairCmd)
	for _, c := range []struct{ name, short string }{
		{"stop", "Stop the current mission"},
		{"pause", "Pause the current mission"},
		{"resume", "Resume a paused mission"},
		{"dock", "Send the robot back to its dock"},
		{"find", "Make the robot play a sound"},
	} {
		Cmd.AddCommand(commandCmd(c.name, c.short))
	}
	roomsCmd.AddCommand(roomsNameCmd, roomsSyncCmd)
	cleanCmd.Flags().StringSliceVar(&cleanRooms, "room", nil, "room name or region id; repeat for several, cleaned in order")
}

func commandCmd(command, short string) *cobra.Command {
	return &cobra.Command{
		Use:   command,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendCommand(cmd, command)
		},
	}
}

func sendCommand(cmd *cobra.Command, command string) error {
	_, err := myhome.TheClient.CallE(cmd.Context(), myhome.VacuumCommand,
		&myhome.VacuumCommandParams{Robot: robotFlag, Command: command})
	if err != nil {
		return err
	}
	fmt.Printf("%s: acknowledged by the robot\n", command)
	return nil
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List the configured robot",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := myhome.TheClient.CallE(cmd.Context(), myhome.VacuumList, nil)
		if err != nil {
			return err
		}
		res, ok := out.(*myhome.VacuumListResult)
		if !ok {
			return fmt.Errorf("unexpected result type %T", out)
		}
		if len(res.Robots) == 0 {
			fmt.Println("No robot vacuum configured. Run 'myhome ctl vacuum pair' and put its output in the daemon's .env.")
			return nil
		}
		for _, r := range res.Robots {
			printRobot(r)
		}
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show a robot's live status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := myhome.TheClient.CallE(cmd.Context(), myhome.VacuumStatusGet, &myhome.VacuumParams{Robot: robotFlag})
		if err != nil {
			return err
		}
		r, ok := out.(*myhome.VacuumRobot)
		if !ok {
			return fmt.Errorf("unexpected result type %T", out)
		}
		printRobot(*r)
		return nil
	},
}

func printRobot(r myhome.VacuumRobot) {
	conn := "connected"
	if !r.Connected {
		conn = "NOT connected"
	}
	fmt.Printf("%s (%s) at %s: %s\n", r.Name, r.ID, r.Host, conn)
	s := r.Status
	if s == nil {
		fmt.Println("  no status received yet")
		return
	}
	fmt.Printf("  state:   %s (cycle %s)\n", myhome.VacuumPhaseText(s.Phase), s.Cycle)
	fmt.Printf("  battery: %d%%\n", s.Battery)
	bin := "ok"
	switch {
	case !s.BinPresent:
		bin = "MISSING"
	case s.BinFull:
		bin = "FULL"
	}
	fmt.Printf("  bin:     %s\n", bin)
	if s.Error != 0 {
		fmt.Printf("  error:   %d\n", s.Error)
	}
	if s.MissionMinutes > 0 {
		fmt.Printf("  mission: %d min\n", s.MissionMinutes)
	}
	fmt.Printf("  updated: %s\n", s.UpdatedAt.Format("2006-01-02 15:04:05"))
}

var cleanRooms []string

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean the whole home, or the given rooms",
	Long: `Without --room, starts a whole-home clean.
With --room (repeatable), cleans those rooms in order; see 'vacuum rooms'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(cleanRooms) == 0 {
			return sendCommand(cmd, "start")
		}
		_, err := myhome.TheClient.CallE(cmd.Context(), myhome.VacuumClean,
			&myhome.VacuumCleanParams{Robot: robotFlag, Rooms: cleanRooms})
		if err != nil {
			return err
		}
		fmt.Printf("cleaning %s: acknowledged by the robot\n", strings.Join(cleanRooms, ", "))
		return nil
	},
}

var roomsCmd = &cobra.Command{
	Use:   "rooms",
	Short: "List the robot's rooms",
	Long: `Lists the robot's rooms (regions).

Rooms come from the iRobot cloud account when MYHOME_IROBOT_EMAIL and
MYHOME_IROBOT_PASSWORD are set in the daemon's .env: the daemon fetches them
once at start, and 'vacuum rooms sync' fetches them again. The cloud is then
the source of truth for room names.

Without the account, rooms are learned whenever a room clean is started,
including from the iRobot app, and named with 'vacuum rooms name'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := myhome.TheClient.CallE(cmd.Context(), myhome.VacuumRooms, &myhome.VacuumParams{Robot: robotFlag})
		if err != nil {
			return err
		}
		res, ok := out.(*myhome.VacuumRoomsResult)
		if !ok {
			return fmt.Errorf("unexpected result type %T", out)
		}
		printRooms(os.Stdout, res)
		return nil
	},
}

// printRooms prints vacuum.rooms. Room ids from the cloud are presented as
// unverified when the cloud's map version differs from the robot's (#628).
func printRooms(w io.Writer, res *myhome.VacuumRoomsResult) {
	if len(res.Regions) == 0 {
		if res.CloudNames {
			fmt.Fprintln(w, "No room known yet. Run 'myhome ctl vacuum rooms sync' to fetch them from the iRobot cloud.")
		} else {
			fmt.Fprintln(w, "No room known yet. Set MYHOME_IROBOT_EMAIL / MYHOME_IROBOT_PASSWORD in the daemon's .env and run")
			fmt.Fprintln(w, "'myhome ctl vacuum rooms sync', or start a room clean from the iRobot app once.")
		}
		return
	}
	fmt.Fprintf(w, "%-8s %-5s %-22s %-14s %-6s %s\n", "REGION", "TYPE", "NAME", "KIND", "FROM", "MAP")
	type mapVersions struct{ cloud, robot string }
	mismatch := map[string]mapVersions{} // map label -> versions, for cloud rooms only
	var order []string
	for _, r := range res.Regions {
		name, kind := r.Name, r.Kind
		if name == "" {
			name = "-"
		}
		if kind == "" {
			kind = "-"
		}
		label := r.MapID
		if r.MapName != "" {
			label = r.MapName + " (" + r.MapID + ")"
		}
		note := ""
		switch {
		case r.Stale && r.Source == myhome.VacuumSourceCloud:
			note = "  [stale: no longer in the iRobot cloud]"
		case r.Stale:
			note = "  [stale: map changed]"
		case r.Unverified:
			note = "  [unverified]"
			if _, seen := mismatch[label]; !seen {
				order = append(order, label)
			}
			mismatch[label] = mapVersions{r.MapVersion, r.RobotMapVersion}
		}
		fmt.Fprintf(w, "%-8s %-5s %-22s %-14s %-6s %s%s\n", r.ID, r.Type, name, kind, r.Source, label, note)
	}
	for _, label := range order {
		v := mismatch[label]
		robot := v.robot
		if robot == "" {
			robot = "no version yet (not connected?)"
		}
		fmt.Fprintf(w, "\nMap %s: ids come from iRobot cloud map version %s; the robot reports %s; unverified.\n", label, v.cloud, robot)
		fmt.Fprintln(w, "A room clean always sends the robot's own version.")
	}
	if res.CloudNames {
		fmt.Fprintln(w, "\nRoom names come from the iRobot cloud ('vacuum rooms sync' refreshes them).")
	}
}

var roomsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Fetch the robot's rooms from the iRobot cloud account",
	Long: `Asks the daemon to log in to the iRobot cloud account (MYHOME_IROBOT_EMAIL /
MYHOME_IROBOT_PASSWORD in its .env) and fetch the robot's maps and rooms.

One login attempt, no retry. Room names from the cloud replace local names; a
room the cloud no longer lists is marked stale. On any failure the rooms already
stored are kept. Only plain email+password accounts work (not Google/Apple
sign-in or two-factor).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := myhome.TheClient.CallE(cmd.Context(), myhome.VacuumRoomsSync, &myhome.VacuumParams{Robot: robotFlag})
		if err != nil {
			return err
		}
		res, ok := out.(*myhome.VacuumRoomsSyncResult)
		if !ok {
			return fmt.Errorf("unexpected result type %T", out)
		}
		printSync(os.Stdout, res)
		return nil
	},
}

func printSync(w io.Writer, res *myhome.VacuumRoomsSyncResult) {
	for _, m := range res.Maps {
		robot := m.RobotVersion
		if robot == "" {
			robot = "not reported"
		}
		fmt.Fprintf(w, "map %q (%s): %d rooms, cloud version %s, robot version %s\n", m.Name, m.ID, m.Rooms, m.CloudVersion, robot)
	}
	fmt.Fprintf(w, "%d added, %d updated, %d now stale. See 'myhome ctl vacuum rooms'.\n", res.Added, res.Updated, res.Stale)
}

var roomsNameCmd = &cobra.Command{
	Use:   "name <region-id> <name>",
	Short: "Name a room the iRobot cloud did not provide",
	Long: `Gives a room a local name. When the iRobot account is configured, rooms from
the cloud cannot be renamed here: their names come from the cloud.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.TheClient.CallE(cmd.Context(), myhome.VacuumRoomName,
			&myhome.VacuumRoomNameParams{Robot: robotFlag, RegionID: args[0], Name: args[1]})
		if err != nil {
			return err
		}
		fmt.Printf("region %s is now %q\n", args[0], args[1])
		return nil
	},
}

var pairCmd = &cobra.Command{
	Use:   "pair [robot-ip]",
	Short: "Get a robot's id and local password for the daemon's .env (hold its HOME button first)",
	Long: `Fetches the robot's id (BLID) and local password, and prints them as the
MYHOME_VACUUM_* lines the daemon reads from its .env file.

1. Put the robot on its dock.
2. Hold its HOME button about 2 seconds, until it plays a tone.
3. Within 2 minutes, run this command on a host on the robot's LAN.

Without robot-ip, the robot is found by discovery and must be the only one answering.

This command talks to the robot directly, not through the daemon, and writes
nothing anywhere: copy the printed lines into /var/lib/myhome/.env (or enter
them at 'sudo dpkg-reconfigure myhome'), then restart the daemon.
Standard output carries only those lines, so it can be redirected; it
contains the password: do not paste it into logs, issues or chats.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host := ""
		if len(args) == 1 {
			host = args[0]
		}
		info, password, err := roomba.Pair(cmd.Context(), host)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Robot %q (%s) answered. Put these lines in the daemon's .env, then restart it:\n", info.Name, info.IP)
		fmt.Printf("MYHOME_VACUUM_BLID=%s\n", info.BLID)
		fmt.Printf("MYHOME_VACUUM_PASSWORD=%s\n", password)
		if host != "" {
			// Only pin the address when the operator chose it: discovery
			// follows the robot when its DHCP-assigned address changes.
			fmt.Printf("MYHOME_VACUUM_HOST=%s\n", host)
		}
		return nil
	},
}
