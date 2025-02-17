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
		ctx := options.CommandLineContext()

		out, err := options.MyHomeClient.CallE(ctx, "group.getdevices", name)
		if err != nil {
			return err
		}
		devices := out.(*[]myhome.Device)
		if options.Flags.Json {
			s, err := json.Marshal(devices)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			for _, device := range *devices {
				fmt.Println(device)
			}
		}
		return nil
	},
}
