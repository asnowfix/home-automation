package group

import (
	"encoding/json"
	"fmt"
	"homectl/options"
	"myhome/devices"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := options.MyHomeClient.CallE("group.list", nil)
		if err != nil {
			return err
		}
		groups, ok := out.(*[]devices.Group)
		if !ok {
			panic("unexpected format (failed to cast groups)")
		}
		if options.Flags.Json {
			s, err := json.Marshal(groups)
			if err != nil {
				panic(err)
			}
			fmt.Println(string(s))
		} else {
			fmt.Println("Groups:")
			for _, group := range *groups {
				fmt.Println(group)
			}
		}
		return nil
	},
}
