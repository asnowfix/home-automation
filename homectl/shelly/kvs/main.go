package kvs

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "kvs",
	Short: "Manage Shelly devices Key-Value Store",
	Run: func(cmd *cobra.Command, args []string) {
	},
}