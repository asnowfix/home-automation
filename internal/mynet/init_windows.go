//go:build windows

package mynet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sys/windows"
)

// firewallRules defines the Windows Firewall rules needed for multicast
var firewallRules = []struct {
	name       string
	port       int
	protocol   int
	remoteAddr string
	desc       string
}{
	{
		name:       "MyHome mDNS IPv4",
		port:       5353,
		protocol:   windows.IPPROTO_UDP,
		remoteAddr: "224.0.0.0/4",
		desc:       "MyHome Automation mDNS IPv4 Multicast",
	},
	{
		name:       "MyHome mDNS IPv6",
		port:       5353,
		protocol:   windows.IPPROTO_UDP,
		remoteAddr: "ff00::/8",
		desc:       "MyHome Automation mDNS IPv6 Multicast",
	},
}

var (
	log    logr.Logger
	once   sync.Once
	exited bool
)

// InitializeFirewall sets up the firewall rules for multicast communication
func InitializeFirewall(logger logr.Logger) error {
	var initErr error
	once.Do(func() {
		log = logger.WithName("firewall")
		initErr = initialize()
	})
	return initErr
}

func initialize() error {
	log := log.WithValues("component", "init")
	// Get the current executable path
	exe, err := os.Executable()
	if err != nil {
		log.Error(err, "Failed to get executable path")
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err := filepath.Abs(exe)
	if err != nil {
		log.Error(err, "Failed to get absolute path")
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	log.V(1).Info("Initializing Windows Firewall rules", "executable", exePath)

	// Check if rules already exist
	existing, err := listExistingRules()
	if err != nil {
		log.Error(err, "Failed to list existing firewall rules")
	}

	// Create firewall rules
	for _, rule := range firewallRules {
		logger := log.WithValues(
			"rule", rule.name,
			"port", rule.port,
			"protocol", rule.protocol,
			"remoteAddr", rule.remoteAddr,
		)

		if ruleExists(existing, rule.name) {
			logger.V(1).Info("Firewall rule already exists")
			continue
		}

		err = addFirewallRule(rule.name, exePath, rule.port, rule.protocol, rule.remoteAddr, rule.desc)
		if err != nil {
			logger.Error(err, "Failed to add firewall rule")
		} else {
			logger.V(1).Info("Added firewall rule")
		}
	}

	// Set up cleanup on program exit
	setupCleanup()
	return nil
}

func setupCleanup() {
	// TODO: Implement cleanup at install
	// go func() {
	// 	c := make(chan os.Signal, 1)
	// 	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	// 	<-c
	// 	if !exited {
	// 		exited = true
	// 		cleanup()
	// 		os.Exit(0)
	// 	}
	// }()
}

// cleanup removes the firewall rules when the program exits
func cleanup() {
	log := log.WithValues("component", "cleanup")
	log.V(1).Info("Cleaning up firewall rules")

	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	if err != nil {
		log.Error(err, "CoInitializeEx failed during cleanup")
		return
	}
	defer windows.CoUninitialize()

	for _, rule := range firewallRules {
		logger := log.WithValues("rule", rule.name)
		if err := removeFirewallRule(rule.name); err != nil {
			logger.Error(err, "Failed to remove firewall rule")
		} else {
			logger.V(1).Info("Removed firewall rule")
		}
	}
}

func removeFirewallRule(name string) error {
	args := []string{
		"advfirewall", "firewall", "delete", "rule",
		"name=" + name,
	}

	err := windows.ShellExecute(0, windows.StringToUTF16Ptr("runas"),
		windows.StringToUTF16Ptr("netsh"),
		windows.StringToUTF16Ptr(windowsJoinArgs(args)),
		nil, windows.SW_HIDE)
	if err != nil {
		return fmt.Errorf("failed to remove firewall rule: %w", err)
	}
	return nil
}

func addFirewallRule(name, program string, port, protocol int, remoteAddr, desc string) error {
	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	if err != nil {
		return fmt.Errorf("CoInitializeEx failed: %w", err)
	}
	defer windows.CoUninitialize()

	args := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=" + name,
		"dir=in",
		"action=allow",
		"program=" + program,
		fmt.Sprintf("protocol=%d", protocol),
		fmt.Sprintf("localport=%d", port),
		"remoteip=" + remoteAddr,
		"enable=yes",
		"description=" + desc,
	}

	err = windows.ShellExecute(0, windows.StringToUTF16Ptr("runas"),
		windows.StringToUTF16Ptr("netsh"),
		windows.StringToUTF16Ptr(windowsJoinArgs(args)),
		nil, windows.SW_HIDE)
	if err != nil {
		return fmt.Errorf("failed to add firewall rule: %w", err)
	}

	return nil
}

func listExistingRules() ([]string, error) {
	tempFile := filepath.Join(os.TempDir(), "firewall_rules.txt")

	// Use cmd.exe /c to handle redirection properly
	cmdArgs := fmt.Sprintf(`netsh advfirewall firewall show rule name=MyHome* > "%s"`, tempFile)
	err := windows.ShellExecute(0, windows.StringToUTF16Ptr("runas"),
		windows.StringToUTF16Ptr("cmd.exe"),
		windows.StringToUTF16Ptr("/c "+cmdArgs),
		nil, windows.SW_HIDE)
	if err != nil {
		return nil, fmt.Errorf("failed to list firewall rules: %w", err)
	}

	// Wait a bit for the file to be written
	time.Sleep(100 * time.Millisecond)

	// Read the rules file
	content, err := os.ReadFile(tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read rules file: %w", err)
	}
	defer os.Remove(tempFile)

	var rules []string
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Rule Name:") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "Rule Name:"))
			rules = append(rules, name)
		}
	}

	return rules, nil
}

func ruleExists(rules []string, name string) bool {
	for _, rule := range rules {
		if rule == name {
			return true
		}
	}
	return false
}

func windowsJoinArgs(args []string) string {
	var result string
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		if strings.Contains(arg, " ") {
			result += `"` + arg + `"`
		} else {
			result += arg
		}
	}
	return result
}
