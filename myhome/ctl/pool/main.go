package pool

import (
	"github.com/spf13/cobra"
)

// poolCmd is the root command for pool pump management
var poolCmd = &cobra.Command{
	Use:   "pool",
	Short: "Manage pool pump automation",
	Long:  `Configure and control the pool pump system with multiple devices running the same script.`,
}

// PoolCmd returns the pool command (exported for registration)
func PoolCmd() *cobra.Command {
	return poolCmd
}
