package script

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "script",
	Short: "Manage scripts running on Shelly devices",
	Args:  cobra.NoArgs,
}

// localScriptsDir and noLocalScripts back --local-scripts-dir and
// --no-local-scripts, shared by `script upload` and `script update` (the
// CLI upload path and the fleet-upload path invoked by the .deb postinst).
// See localScriptsDirEffective in start-stop-delete.go for how they combine.
//
// Default is disabled (empty dir): a stale file lying around a working
// directory must never silently shadow the shipped script, especially on
// the NAS where the packaged service's postinst runs `script update '*'`
// unattended. See issue #457.
var localScriptsDir string
var noLocalScripts bool

func init() {
	Cmd.PersistentFlags().StringVar(&localScriptsDir, "local-scripts-dir", "",
		"Load device scripts from this flat directory when a same-named file exists there, "+
			"before falling back to the embedded copy (dev convenience, avoids a rebuild per edit). "+
			"Empty (default) disables local loading entirely.")
	Cmd.PersistentFlags().BoolVar(&noLocalScripts, "no-local-scripts", false,
		"Force embedded-only script loading, overriding --local-scripts-dir")
}

// localScriptsDirEffective returns the directory LoadScript should consult,
// honoring --no-local-scripts as a hard override even if a directory was
// configured.
func localScriptsDirEffective() string {
	if noLocalScripts {
		return ""
	}
	return localScriptsDir
}
