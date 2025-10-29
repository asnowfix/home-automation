package sfr

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "sfr",
	Short: "SFR Box management commands",
	Args:  cobra.NoArgs,
}
