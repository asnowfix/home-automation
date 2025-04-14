package follow

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"slices"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	FollowCmd.PersistentFlags().Uint32VarP(&options.Flags.SwitchId, "switch-id", "i", 0, "Switch Id, if relevant")
	UnfollowCmd.PersistentFlags().Uint32VarP(&options.Flags.SwitchId, "switch-id", "i", 0, "Switch Id, if relevant")
}

var FollowCmd = &cobra.Command{
	Use:   "follow",
	Short: "Start following other device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return devicesDo(cmd.Context(), follow, args[0], args[1])
	},
}

var UnfollowCmd = &cobra.Command{
	Use:   "unfollow",
	Short: "Stop following other device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return devicesDo(cmd.Context(), unfollow, args[0], args[1])
	},
}

type doFollowFunc func(ctx context.Context, log logr.Logger, via types.Channel, follower *shelly.Device, following []*shelly.Device) (any, error)

func devicesDo(ctx context.Context, f doFollowFunc, follower, following string) error {
	followingDevices, err := myhome.TheClient.LookupDevices(ctx, following)
	if err != nil {
		return err
	}

	return myhome.Foreach(ctx, hlog.Logger, follower, types.ChannelDefault, func(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
		return f(ctx, log, via, device, shelly.Devices(ctx, log, *followingDevices))
	}, []string{})
}

const followingKey = "following"

func follow(ctx context.Context, log logr.Logger, via types.Channel, follower *shelly.Device, following []*shelly.Device) (any, error) {
	f := make([]string, 0)
	fv, err := kvs.GetValue(ctx, log, via, follower, followingKey)
	if err != nil {
		log.Info("Unable to get list of followed devices. Assuming none", "error", err)
	} else {
		json.Unmarshal([]byte(fv.Value), &f)
	}

	log.Info("Will follow", "follower", follower.Id(), "following", following)
	for _, d := range following {
		log.Info("Adding followed device to list", "device", d.Id(), "list", following)
		// add device id to the list , if not already present
		if !slices.Contains(f, d.Id()) {
			f = append(f, d.Id())
		}
	}

	log.Info("Will now follow", "follower", follower.Id(), "following", f)
	buf, err := json.Marshal(f)
	if err != nil {
		log.Error(err, "Unable to marshal list of followed devices")
		return nil, err
	}

	kvsStatus, err := kvs.SetKeyValue(ctx, log, via, follower, followingKey, string(buf))
	if err != nil {
		log.Error(err, "Unable to set list of followed devices")
		return nil, err
	}
	log.Info("Set list of followed devices", "status", kvsStatus)

	// script, err := scripts.Load(follower, "following.js")
	// if err != nil {
	// 	log.Error(err, "Unable to load script following.js")
	// 	return nil, err
	// }

	// status, err := scripts.Enable(follower, script)
	// if err != nil {
	// 	log.Error(err, "Unable to enable script following.js")
	// 	return nil, err
	// }

	return nil, nil

}

func unfollow(ctx context.Context, log logr.Logger, via types.Channel, follower *shelly.Device, following []*shelly.Device) (any, error) {
	return nil, fmt.Errorf("not implemented")
}
