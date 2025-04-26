package sys

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "sys",
	Short: "Shelly device system operations",
	Args:  cobra.NoArgs,
}
