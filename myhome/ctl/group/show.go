package group

import (
	"encoding/json"
	"fmt"
	"myhome"
	"myhome/ctl/options"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	Cmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show device groups",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		out, err := myhome.TheClient.CallE(cmd.Context(), myhome.GroupShow, name)
		if err != nil {
			return err
		}
		group, ok := out.(*myhome.Group)
		if !ok {
			return fmt.Errorf("expected myhome.Group, got %T", out)
		}
		if options.Flags.Json {
			s, err := json.Marshal(group)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			s, err := yaml.Marshal(group)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		}
		return nil
	},
}
