package group

import (
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"reflect"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		out, err := options.MyHomeClient.CallE("group.list", nil)
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		groups, ok := out.(*myhome.Groups)
		if !ok {
			panic("unexpected format (failed to cast groups)")
		}
		if options.Flags.Json {
			s, err := json.Marshal(groups)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			fmt.Println("Groups:")
			for _, group := range groups.Groups {
				fmt.Println(group)
			}
		}
		return nil
	},
}
