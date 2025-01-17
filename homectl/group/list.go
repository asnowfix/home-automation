package group

import (
	"encoding/json"
	"fmt"
	"homectl/options"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List groups",
	Run: func(cmd *cobra.Command, args []string) {
		out, err := myhomeClient.CallE("group.list", nil)
		if err != nil {
			panic(err)
		}
		groups := out.([]string)
		if options.Flags.Json {
			s, err := json.Marshal(groups)
			if err != nil {
				panic(err)
			}
			fmt.Println(string(s))
		} else {
			for _, group := range groups {
				fmt.Println(group)
			}
		}
	},
}
