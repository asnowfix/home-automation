package listen

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "listen",
	Short: "Configure device listening for various protocols",
	Long:  "Configure device listening for BLE devices and status mirroring",
}

func init() {
	Cmd.AddCommand(bluCmd)
	Cmd.AddCommand(statusCmd)
}
