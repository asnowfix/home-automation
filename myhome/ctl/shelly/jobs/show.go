package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"reflect"
	"schedule"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/types"
)

var showCtl = &cobra.Command{
	Use:   "show",
	Short: "Show Shelly devices scheduled jobs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, showOneDeviceJobs, options.Args(args))
		return err
	},
}

func showOneDeviceJobs(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	out, err := schedule.ShowJobs(ctx, log, via, sd)
	if err != nil {
		log.Error(err, "Unable to get scheduled jobs: %v", err)
		return nil, err
	}

	jobs := out.(*schedule.Scheduled)
	
	// Prepare complete output as a single string to avoid interleaving
	var output string
	deviceHeader := fmt.Sprintf("%s (%s)", sd.Name(), sd.Id())
	
	if options.Flags.Json {
		// For JSON, include device info in the output
		jsonOutput := map[string]interface{}{
			"device": deviceHeader,
			"id":     sd.Id(),
			"name":   sd.Name(),
			"jobs":   jobs,
		}
		s, err := json.MarshalIndent(jsonOutput, "", "  ")
		if err != nil {
			return nil, err
		}
		output = string(s)
	} else {
		// For YAML, build complete output with header and jobs
		s, err := yaml.Marshal(jobs)
		if err != nil {
			return nil, err
		}
		output = fmt.Sprintf("# %s\n%s", deviceHeader, string(s))
	}
	
	// Print complete output atomically
	fmt.Print(output)

	return jobs, nil
}
