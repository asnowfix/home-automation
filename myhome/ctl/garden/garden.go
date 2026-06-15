package garden

import "github.com/spf13/cobra"

var gardenCmd = &cobra.Command{
	Use:   "garden",
	Short: "Manage garden sprinkler automation",
	Long:  `Configure and control the garden sprinkler system with ET0 water-balance scheduling.`,
}

// GardenCmd returns the garden command for registration in the ctl tree.
func GardenCmd() *cobra.Command {
	return gardenCmd
}
