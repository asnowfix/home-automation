package script

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(prometheusScrapeConfigCmd)
}

var prometheusScrapeConfigCmd = &cobra.Command{
	Use:     "prometheus-scrape-config",
	Short:   "Generate Prometheus scrape_configs: for Shelly devices running prometheus.js",
	Aliases: []string{"prom-config", "pc"},
	Args:    cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		if len(args) == 0 {
			return fmt.Errorf("device glob or identifier required")
		}
		_, err := myhome.Foreach(cmd.Context(), log, args[0], options.Via, generatePrometheusScrapeConfig, options.Args(args))
		return err
	},
}

func generatePrometheusScrapeConfig(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", device.Id(), device)
	}

	loaded, err := script.ListLoaded(ctx, via, sd)
	if err != nil {
		log.Error(err, "Unable to list scripts", "device", sd.Id())
		return nil, nil
	}

	for _, s := range loaded {
		if s.Name == "prometheus.js" && s.Running {
			fmt.Printf(`
- job_name: 'shelly_%s'
  metrics_path: /script/%d/metrics
  static_configs:
    - targets: ['%s']
`, sd.Id(), s.Id, sd.Ip())
		}
	}
	return nil, nil
}
