package solar

import (
	"github.com/spf13/cobra"
)

// solarCmd is the root command for solar-energy related queries.
var solarCmd = &cobra.Command{
	Use:   "solar",
	Short: "Query solar-energy claimers",
	Long:  `Query the daemon's minimal energy-claimers registry (see issue #404).`,
}

// SolarCmd returns the solar command (exported for registration).
func SolarCmd() *cobra.Command {
	return solarCmd
}
