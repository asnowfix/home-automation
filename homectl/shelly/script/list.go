package script

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

func init() {
	Cmd.AddCommand(listCtl)
}

var listCtl = &cobra.Command{
	Use:   "list",
	Short: "Report status of every scripts loaded on the given Shelly device(s)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		out, err := myhome.Foreach(cmd.Context(), log, args[0], options.Via, doList, options.Args(args))
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		scripts := out.([]script.Status)
		if options.Flags.Json {
			s, err := json.Marshal(scripts)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			s, err := yaml.Marshal(scripts)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		}
		return nil
	},
}

func doList(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return script.ListAll(ctx, device, via)
}
