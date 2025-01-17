package group

import (
	"context"
	"hlog"
	"homectl/options"
	"myhome"

	"github.com/spf13/cobra"
)

var myhomeClient myhome.Proxy

var Cmd = &cobra.Command{
	Use:   "group",
	Short: "Manage device groups",
	Args:  cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		log := hlog.Init()
		myhomeClient, err = myhome.NewClientProxyE(context.Background(), log, options.Flags.MqttBroker)
		if err != nil {
			return err
		}
		return nil
	},
}
