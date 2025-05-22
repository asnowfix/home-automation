package script

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "script",
	Short: "Manage scripts running on Shelly devices",
	Args:  cobra.NoArgs,
}
