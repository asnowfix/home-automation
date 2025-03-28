package group

import (
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"reflect"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	Cmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger

		out, err := myhome.TheClient.CallE(cmd.Context(), myhome.GroupList, nil)
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		groups := out.(*myhome.Groups)
		if options.Flags.Json {
			s, err := json.Marshal(groups)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			s, err := yaml.Marshal(groups)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		}
		return nil
	},
}
