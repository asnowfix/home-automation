package group

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "group",
	Short: "Manage device groups",
	Args:  cobra.NoArgs,
}
