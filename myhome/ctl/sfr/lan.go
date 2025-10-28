package sfr

import (
	"myhome/ctl/options"
	"pkg/sfr"

	"github.com/spf13/cobra"
)

var lanCmd = &cobra.Command{
	Use:   "lan",
	Short: "SFR Box LAN management commands",
	Args:  cobra.NoArgs,
}

var getHostsListCmd = &cobra.Command{
	Use:   "getHostsList",
	Short: "Get the list of hosts connected to the SFR Box LAN",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts, err := sfr.GetHostsList()
		if err != nil {
			return err
		}
		return options.PrintResult(hosts)
	},
}

func init() {
	Cmd.AddCommand(lanCmd)
	lanCmd.AddCommand(getHostsListCmd)
}
