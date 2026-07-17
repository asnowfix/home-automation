package daemon

import (
	"github.com/spf13/cobra"
)

func init() {
}

var disableDeviceManager bool

// IsDaemonAnnotation, when set to "true" on a cobra.Command's Annotations,
// marks that command (and, per IsDaemonCommand, every command nested under
// it) as part of the daemon tree. Callers use this instead of matching on
// cmd.Name()/cmd.Parent().Name(), which breaks as soon as a subcommand is
// nested more than one level below "daemon".
const IsDaemonAnnotation = "myhome.daemon"

var Cmd = &cobra.Command{
	Use:   "daemon",
	Short: "MyHome Daemon",
	Long:  "MyHome Daemon, with embedded MQTT broker and persistent device manager",
	Args:  cobra.NoArgs,
	Annotations: map[string]string{
		IsDaemonAnnotation: "true",
	},
}

// IsDaemonCommand reports whether cmd, or any of its ancestors, is part of
// the daemon command tree (i.e. carries IsDaemonAnnotation). This lets
// callers apply daemon-specific defaults (e.g. verbose-by-default logging)
// to "daemon run", "daemon install", and any subcommand nested arbitrarily
// deep under them, without hardcoding command names/depth.
func IsDaemonCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations != nil && c.Annotations[IsDaemonAnnotation] == "true" {
			return true
		}
	}
	return false
}
