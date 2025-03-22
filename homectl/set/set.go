package set

import (
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(setShellyCmd)
}

var Cmd = &cobra.Command{
	Use:   "set",
	Short: "Set device attributes",
	Args:  cobra.NoArgs,
}
