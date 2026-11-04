// Package fetch provides the `myhome ctl fetch` command: list and delete
// the daemon's persisted fetch-and-transform subscriptions (#465). It talks
// to the daemon exclusively via the fetch.list / fetch.delete RPC methods —
// no business logic lives here, per CLAUDE.md's three-tier layer rule.
package fetch

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/spf13/cobra"
)

// Cmd is the root "fetch" sub-command registered under "myhome ctl".
var Cmd = &cobra.Command{
	Use:   "fetch",
	Short: "Manage daemon-side fetch-and-transform subscriptions (#465)",
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(deleteCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List fetch subscriptions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		result, err := myhome.TheClient.CallE(ctx, myhome.FetchList, nil)
		if err != nil {
			return err
		}
		list, ok := result.(*myhome.FetchListResult)
		if !ok {
			return fmt.Errorf("unexpected result type: %T", result)
		}

		if options.Flags.Json {
			return options.PrintResult(list)
		}

		if len(list.Subscriptions) == 0 {
			fmt.Println("No fetch subscriptions found")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "DEVICE\tNAME\tURL\tTOPIC\tINTERVAL\tLAST SEEN\tLAST OK")
		fmt.Fprintln(w, "------\t----\t---\t-----\t--------\t---------\t-------")
		for _, sub := range list.Subscriptions {
			lastSeen := "(never, this process)"
			if sub.LastSeen != nil {
				lastSeen = *sub.LastSeen
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%ds\t%s\t%v\n",
				sub.DeviceID, sub.Name, sub.URL, sub.Topic, sub.IntervalSeconds, lastSeen, sub.LastFetchOK)
		}
		return w.Flush()
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete <device-id> <name>",
	Short: "Delete a fetch subscription",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		params := &myhome.FetchDeleteParams{
			DeviceID: args[0],
			Name:     args[1],
		}
		result, err := myhome.TheClient.CallE(ctx, myhome.FetchDelete, params)
		if err != nil {
			return err
		}
		deleted, ok := result.(*myhome.FetchDeleteResult)
		if !ok {
			return fmt.Errorf("unexpected result type: %T", result)
		}

		fmt.Printf("Deleted fetch subscription: %s/%s\n", deleted.DeviceID, deleted.Name)
		return nil
	},
}
