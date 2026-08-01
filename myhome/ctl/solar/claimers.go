package solar

import (
	"fmt"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/spf13/cobra"
)

// claimersCmd calls the myhome.SolarClaimersList RPC (registered by the
// daemon; see myhome/daemon/solar_rpc.go) and prints each registered
// energy claimer's static identity plus, where available, a live
// active/speed read. This is a static-identity registry, not a
// live-arbitration view — see issue #404.
var claimersCmd = &cobra.Command{
	Use:   "claimers",
	Short: "List registered solar-energy claimers",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		out, err := myhome.TheClient.CallE(ctx, myhome.SolarClaimersList, nil)
		if err != nil {
			return fmt.Errorf("failed to list solar claimers: %w", err)
		}
		result, ok := out.(*myhome.SolarClaimersListResult)
		if !ok {
			return fmt.Errorf("unexpected result type %T", out)
		}

		if len(result.Claimers) == 0 {
			fmt.Println("No solar-energy claimers registered.")
			return nil
		}

		fmt.Println("Solar-Energy Claimers")
		fmt.Println("=====================")
		for _, c := range result.Claimers {
			status := "idle"
			if c.Active {
				status = "active"
				if c.ActiveSpeed != "" {
					status = fmt.Sprintf("active (%s)", c.ActiveSpeed)
				}
			}
			fmt.Printf("• %s (%s) — %s\n", c.Name, c.DeviceID, status)
		}

		return nil
	},
}

func init() {
	solarCmd.AddCommand(claimersCmd)
}
