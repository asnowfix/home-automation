package set

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "set",
	Short: "Set device settings",
	Args:  cobra.NoArgs,
}
