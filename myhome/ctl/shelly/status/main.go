package status

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/shelly"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

var Cmd = &cobra.Command{
	Use:   "status",
	Short: "Show current status of inputs and switch outputs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doStatus, options.Args(args))
		return err
	},
}

type ComponentStatus struct {
	Name        string  `json:"name,omitempty" yaml:"name,omitempty"`
	State       *bool   `json:"state,omitempty" yaml:"state,omitempty"`             // For inputs
	Output      *bool   `json:"output,omitempty" yaml:"output,omitempty"`           // For switches
	Power       float32 `json:"power,omitempty" yaml:"power,omitempty"`             // For switches with power metering
	Voltage     float32 `json:"voltage,omitempty" yaml:"voltage,omitempty"`         // For switches with power metering
	Current     float32 `json:"current,omitempty" yaml:"current,omitempty"`         // For switches with power metering
	Temperature float32 `json:"temperature,omitempty" yaml:"temperature,omitempty"` // For switches with temperature sensor
}

type DeviceStatus struct {
	Device     string                     `json:"device" yaml:"device"`
	Components map[string]ComponentStatus `json:"components" yaml:"components"`
}

func doStatus(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	// Request components with both config (for names) and status
	components, err := shelly.DoGetComponents(ctx, sd, &shelly.ComponentsRequest{
		Include: []string{"config", "status"},
		Keys:    []string{"input:0", "input:1", "input:2", "input:3", "switch:0", "switch:1", "switch:2", "switch:3"},
	})
	if err != nil {
		return nil, err
	}

	result := DeviceStatus{
		Device:     sd.Name(),
		Components: make(map[string]ComponentStatus),
	}

	// Process inputs
	for id, inputCfg := range []*sswitch.InputConfig{
		components.Config.Input0,
		components.Config.Input1,
		components.Config.Input2,
		components.Config.Input3,
	} {
		if inputCfg == nil {
			continue
		}

		cs := ComponentStatus{}

		if inputCfg.Name != nil && *inputCfg.Name != "" {
			cs.Name = *inputCfg.Name
		}

		// Get status
		inputStatus := []*sswitch.InputStatus{
			components.Status.Input0,
			components.Status.Input1,
			components.Status.Input2,
			components.Status.Input3,
		}[id]

		if inputStatus != nil {
			cs.State = &inputStatus.State
		}

		key := fmt.Sprintf("input:%d", inputCfg.Id)
		result.Components[key] = cs
	}

	// Process switches
	for id, switchCfg := range []*sswitch.Config{
		components.Config.Switch0,
		components.Config.Switch1,
		components.Config.Switch2,
		components.Config.Switch3,
	} {
		if switchCfg == nil {
			continue
		}

		cs := ComponentStatus{}

		if switchCfg.Name != "" {
			cs.Name = switchCfg.Name
		}

		// Get status
		switchStatus := []*sswitch.Status{
			components.Status.Switch0,
			components.Status.Switch1,
			components.Status.Switch2,
			components.Status.Switch3,
		}[id]

		if switchStatus != nil {
			cs.Output = &switchStatus.Output
			cs.Power = switchStatus.Apower
			cs.Voltage = switchStatus.Voltage
			cs.Current = switchStatus.Current
			cs.Temperature = switchStatus.Temperature.Celsius
		}

		key := fmt.Sprintf("switch:%d", switchCfg.Id)
		result.Components[key] = cs
	}

	// Output result
	if options.Flags.Json {
		s, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(result)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}

	return result, nil
}
