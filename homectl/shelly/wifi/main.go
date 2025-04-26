package wifi

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "wifi",
	Short: "Shelly devices WiFi configuration & status",
	Args:  cobra.NoArgs,
}
