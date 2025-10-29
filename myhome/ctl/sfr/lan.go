package sfr

import (
	"fmt"
	"myhome/ctl/options"
	"pkg/sfr"
	"strings"

	"github.com/spf13/cobra"
)

var lanCmd = &cobra.Command{
	Use:   "lan",
	Short: "SFR Box LAN management commands",
	Args:  cobra.NoArgs,
}

var getHostsListCmd = &cobra.Command{
	Use:   "getHostsList [filter]",
	Short: "Get the list of hosts connected to the SFR Box LAN",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts, err := sfr.GetHostsList()
		if err != nil {
			return err
		}
		
		// Apply filter if provided
		if len(args) > 0 && args[0] != "" {
			filtered := filterLanHosts(*hosts, args[0])
			return options.PrintResult(&filtered)
		}
		
		return options.PrintResult(hosts)
	},
}

var getDnsHostListCmd = &cobra.Command{
	Use:   "getDnsHostList [filter]", // without trailing 's', unlike GetHostsList
	Short: "Get the list of DNS hosts configured on the SFR Box",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts, err := sfr.GetDnsHostList()
		if err != nil {
			return err
		}
		
		// Apply filter if provided
		if len(args) > 0 && args[0] != "" {
			filtered := filterDnsHosts(*hosts, args[0])
			return options.PrintResult(&filtered)
		}
		
		return options.PrintResult(hosts)
	},
}

// filterLanHosts filters LanHost entries based on a pattern matching any field
func filterLanHosts(hosts []*sfr.LanHost, pattern string) []*sfr.LanHost {
	pattern = strings.ToLower(pattern)
	filtered := make([]*sfr.LanHost, 0)
	
	for _, host := range hosts {
		// Check if pattern matches any field
		if strings.Contains(strings.ToLower(host.Name), pattern) ||
			strings.Contains(strings.ToLower(host.Ip.String()), pattern) ||
			strings.Contains(strings.ToLower(host.Mac), pattern) ||
			strings.Contains(strings.ToLower(host.Interface), pattern) ||
			strings.Contains(strings.ToLower(host.Type), pattern) ||
			strings.Contains(strings.ToLower(host.Status), pattern) ||
			strings.Contains(strings.ToLower(fmt.Sprintf("%d", host.Probe)), pattern) ||
			strings.Contains(strings.ToLower(fmt.Sprintf("%d", host.Alive)), pattern) {
			filtered = append(filtered, host)
		}
	}
	
	return filtered
}

// filterDnsHosts filters DnsHost entries based on a pattern matching any field
func filterDnsHosts(hosts []*sfr.DnsHost, pattern string) []*sfr.DnsHost {
	pattern = strings.ToLower(pattern)
	filtered := make([]*sfr.DnsHost, 0)
	
	for _, host := range hosts {
		// Check if pattern matches any field
		if strings.Contains(strings.ToLower(host.Name), pattern) ||
			strings.Contains(strings.ToLower(host.Ip.String()), pattern) {
			filtered = append(filtered, host)
		}
	}
	
	return filtered
}

func init() {
	Cmd.AddCommand(lanCmd)
	lanCmd.AddCommand(getHostsListCmd)
	lanCmd.AddCommand(getDnsHostListCmd)
}
