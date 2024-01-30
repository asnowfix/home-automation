package set

import (
	"homectl/set/shelly"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(shelly.Cmd)
}

var Cmd = &cobra.Command{
	Use:   "set",
	Short: "Set devices configurqtion",
}
