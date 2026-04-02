package pool

import (
	"github.com/spf13/cobra"
)

// PoolCmd creates the pool command
func PoolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pool",
		Short: "Manage pool pump automation",
		Long:  `Configure and control the pool pump system with controller and bootstrap devices.`,
	}

	cmd.AddCommand(setupCmd)
	cmd.AddCommand(statusCmd)
	cmd.AddCommand(startCmd)
	cmd.AddCommand(stopCmd)
	cmd.AddCommand(purgeCmd)

	return cmd
}
