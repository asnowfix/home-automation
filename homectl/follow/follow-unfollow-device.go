package follow

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"slices"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var flags struct {
	BleShellyMotion bool
	Device          bool
}

const FOLLOW_KEY_PREFIX = "following"

const BLE_SHELLY_MOTION = "ble-shelly-motion"
const DEVICE = "device"

func init() {
	FollowCmd.PersistentFlags().Uint32VarP(&options.Flags.SwitchId, "switch-id", "i", 0, "Switch Id, if relevant")
	UnfollowCmd.PersistentFlags().Uint32VarP(&options.Flags.SwitchId, "switch-id", "i", 0, "Switch Id, if relevant")

	FollowCmd.PersistentFlags().BoolVarP(&flags.BleShellyMotion, BLE_SHELLY_MOTION, "b", false, "MAC address of BLE Shelly Motion device")
	UnfollowCmd.PersistentFlags().BoolVarP(&flags.BleShellyMotion, BLE_SHELLY_MOTION, "b", false, "MAC address of BLE Shelly Motion device")

	FollowCmd.PersistentFlags().BoolVarP(&flags.Device, DEVICE, "d", false, "Device name / IP address / ID")
	UnfollowCmd.PersistentFlags().BoolVarP(&flags.Device, DEVICE, "d", false, "Device name / IP address / ID")

	FollowCmd.MarkFlagsOneRequired(BLE_SHELLY_MOTION, DEVICE)
	UnfollowCmd.MarkFlagsOneRequired(BLE_SHELLY_MOTION, DEVICE)
	FollowCmd.MarkFlagsMutuallyExclusive(BLE_SHELLY_MOTION, DEVICE)
	UnfollowCmd.MarkFlagsMutuallyExclusive(BLE_SHELLY_MOTION, DEVICE)
}

var FollowCmd = &cobra.Command{
	Use:   "follow",
	Short: "Start following other device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		follower := args[0]
		_, err := devicesDo(cmd.Context(), follow, follower, args[1:])
		return err
	},
}

var UnfollowCmd = &cobra.Command{
	Use:   "unfollow",
	Short: "Stop following other device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		follower := args[0]
		_, err := devicesDo(cmd.Context(), unfollow, follower, args[1:])
		return err
	},
}

type doFollowFunc func(ctx context.Context, log logr.Logger, via types.Channel, follower devices.Device, followKey string, following []string) (any, error)

func devicesDo(ctx context.Context, f doFollowFunc, follower string, args []string) (any, error) {
	log := hlog.Logger
	var followKey string
	var following []string

	if flags.Device {
		log.Info("Following device", "device", args[0])
		if len(args) != 1 {
			return nil, fmt.Errorf("expected 1 argument, got %d", len(args))
		}
		followingDevices, err := myhome.TheClient.LookupDevices(ctx, args[0])
		if err != nil {
			log.Error(err, "Unable to lookup following devices")
			return nil, err
		}
		following = make([]string, 0, len(*followingDevices))
		for _, d := range *followingDevices {
			following = append(following, d.Id())
		}
		followKey = FOLLOW_KEY_PREFIX + "/" + DEVICE
	}

	if flags.BleShellyMotion {
		log.Info("Following BLE Shelly Motion", "mac", args)
		followKey = FOLLOW_KEY_PREFIX + "/" + BLE_SHELLY_MOTION
		following = args
	}

	return myhome.Foreach(ctx, log, follower, options.Via, func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
		return f(ctx, log, via, device, followKey, following)
	}, []string{})
}

func follow(ctx context.Context, log logr.Logger, via types.Channel, follower devices.Device, followKey string, following []string) (any, error) {
	f := make([]string, 0)
	sd, ok := follower.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("follower is not a ShellyDevice")
	}
	fv, err := kvs.GetValue(ctx, log, via, sd, followKey)
	if err != nil {
		log.Info("Unable to get list of followed devices. Assuming none", "error", err)
	} else {
		json.Unmarshal([]byte(fv.Value), &f)
	}

	log.Info("Will follow", "follower", sd.Id(), "following", following)
	for _, d := range following {
		log.Info("Adding followed device to list", "device", d, "list", following)
		// add device id to the list , if not already present
		if !slices.Contains(f, d) {
			f = append(f, d)
		}
	}

	log.Info("Will now follow", "follower", sd.Id(), "following", f)
	buf, err := json.Marshal(f)
	if err != nil {
		log.Error(err, "Unable to marshal list of followed devices")
		return nil, err
	}

	kvsStatus, err := kvs.SetKeyValue(ctx, log, via, sd, followKey, string(buf))
	if err != nil {
		log.Error(err, "Unable to set list of followed devices")
		return nil, err
	}
	log.Info("Set list of followed devices", "status", kvsStatus)

	// script, err := scripts.Load(sd, "following.js")
	// if err != nil {
	// 	log.Error(err, "Unable to load script following.js")
	// 	return nil, err
	// }

	// status, err := scripts.Enable(sd, script)
	// if err != nil {
	// 	log.Error(err, "Unable to enable script following.js")
	// 	return nil, err
	// }

	return nil, nil

}

func unfollow(ctx context.Context, log logr.Logger, via types.Channel, follower devices.Device, followKey string, following []string) (any, error) {
	return nil, fmt.Errorf("not implemented")
}
