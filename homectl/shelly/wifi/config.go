package wifi

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/types"
	"pkg/shelly/wifi"

	"homectl/options"
)

var flags struct {
	Ssid     string
	Password string
	Ip       string
	Netmask  string
	Gateway  string
	Mode     string
	AP       bool
	STA1     bool
	Open     bool
}

func init() {
	Cmd.AddCommand(configCmd)

	configCmd.Flags().BoolVarP(&flags.AP, "ap", "A", false, "Set WiFi parameters for Access Point AP mode (default is Station mode: STA)")
	configCmd.Flags().BoolVarP(&flags.STA1, "sta1", "1", false, "Set WiFi parameters for fallback Station mode: STA1 (default is Station mode: STA)")

	configCmd.Flags().StringVarP(&flags.Ssid, "ssid", "S", "", "WiFi SSID")
	configCmd.Flags().StringVarP(&flags.Password, "password", "P", "", "WiFi password")
	configCmd.Flags().StringVarP(&flags.Ip, "ip", "I", "", "Static IP address (Station mode only)")
	configCmd.Flags().StringVarP(&flags.Netmask, "netmask", "N", "", "Static netmask (Access Point mode only)")
	configCmd.Flags().StringVarP(&flags.Gateway, "gateway", "g", "", "Static gateway (Access Point mode only)")
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Get & set Shelly devices WiFi configuration",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, configOneDevice, options.Args(args))
		return err
	},
}

func configOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	out, err := sd.CallE(ctx, via, wifi.GetConfig.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get WiFi config")
		return nil, err
	}
	config, ok := out.(*wifi.Config)
	if !ok {
		log.Error(nil, "Invalid WiFi config type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid WiFi config type %T (should be *wifi.Config)", out)
	}

	// Update config from flags, if any provided
	var changed bool = false
	var sta wifi.STA
	var sta1 wifi.STA
	var ap wifi.AP
	if config.STA != nil && !flags.AP && !flags.STA1 {
		sta = *config.STA
	}
	if config.STA1 != nil && flags.STA1 {
		sta1 = *config.STA1
	}
	if config.AP != nil && flags.AP {
		ap = *config.AP
	}

	if flags.Ssid != "" {
		sta.SSID = flags.Ssid
		sta1.SSID = flags.Ssid
		ap.SSID = flags.Ssid
		changed = true
	}
	if flags.Password != "" {
		sta.Password = &flags.Password
		sta1.Password = &flags.Password
		ap.Password = &flags.Password
		changed = true
	}

	if flags.Ip != "" {
		sta.IP = &flags.Ip
		sta1.IP = &flags.Ip
		changed = true
	}
	if flags.Netmask != "" {
		sta.Netmask = &flags.Netmask
		sta1.Netmask = &flags.Netmask
		changed = true
	}
	if flags.Gateway != "" {
		sta.Gateway = &flags.Gateway
		sta1.Gateway = &flags.Gateway
		changed = true
	}

	if changed {
		if flags.AP {
			config.AP = &ap
		} else {
			config.AP = nil
		}
		if flags.STA1 {
			config.STA1 = &sta1
		} else {
			config.STA1 = nil
		}
		if !flags.AP && !flags.STA1 {
			config.STA = &sta
		} else {
			config.STA = nil
		}

		// some config was changed: update device
		out, err = sd.CallE(ctx, via, wifi.SetConfig.String(), wifi.SetConfigRequest{
			Config: *config,
		})
		if err != nil {
			log.Error(err, "Unable to set WiFi config")
			return nil, err
		}
		res, ok := out.(*wifi.SetConfigResponse)
		if !ok {
			log.Error(nil, "Invalid WiFi set config response type", "type", reflect.TypeOf(out))
			return nil, fmt.Errorf("invalid WiFi set config response type %T", out)
		}
		if res.Result.RestartRequired {
			sd.CallE(ctx, via, string(shelly.Reboot), nil)
		}

		// get updated config from devices after applied
		out, err = sd.CallE(ctx, via, wifi.GetConfig.String(), nil)
		if err != nil {
			log.Error(err, "Unable to get WiFi config")
			return nil, err
		}
		config, ok = out.(*wifi.Config)
		if !ok {
			log.Error(nil, "Invalid WiFi config type", "type", reflect.TypeOf(out))
			return nil, fmt.Errorf("invalid WiFi config type %T (should be *wifi.Config)", out)
		}
	}

	// Now show the result config
	if options.Flags.Json {
		s, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(config)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}

	return out, nil
}
