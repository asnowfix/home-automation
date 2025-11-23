package script

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(prometheusScrapeConfigCmd)
	prometheusScrapeConfigCmd.Flags().StringVar(&metricsExporterHost, "host", "myhome.local", "Metrics exporter hostname (use mDNS name or IP)")
	prometheusScrapeConfigCmd.Flags().IntVar(&metricsExporterPort, "port", 9100, "Metrics exporter port")
}

var metricsExporterHost string
var metricsExporterPort int

var prometheusScrapeConfigCmd = &cobra.Command{
	Use:     "prometheus-scrape-config",
	Short:   "Generate Prometheus scrape_configs: for the MyHome metrics exporter (centralized MQTT-based metrics collection)",
	Aliases: []string{"prom-config", "pc"},
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		generatePrometheusScrapeConfig()
		return nil
	},
}

func generatePrometheusScrapeConfig() {
	fmt.Printf(`scrape_configs:
  - job_name: 'shelly'
    scrape_interval: 30s
    static_configs:
      - targets: ['%s:%d']
`, metricsExporterHost, metricsExporterPort)
}
