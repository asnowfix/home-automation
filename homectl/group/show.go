package group

import (
	"encoding/json"
	"fmt"
	"homectl/options"
	"myhome"

	"github.com/spf13/cobra"
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
		out, err := options.MyHomeClient.CallE(cmd.Context(), myhome.GroupShow, name)
		if err != nil {
			return err
		}
		group := out.(*myhome.Group)
		if options.Flags.Json {
			s, err := json.Marshal(group)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			fmt.Println("name:", group.Name)
			fmt.Println("description:", group.Description)
			fmt.Println("devices:")
			for _, device := range group.Devices {
				fmt.Println("-" + string(device.Name))
			}
		}
		return nil
	},
}
