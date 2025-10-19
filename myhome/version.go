package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var Program string
var Repo string
var Version string
var Commit string

func init() {
	Cmd.AddCommand(versionCmd)
}

// getVersion returns the version string, using git describe if build-time version is not set
func getVersion() string {
	// If Version was set at build time, use it
	if Version != "" {
		return Version
	}
	
	// Otherwise, try to get version from git describe
	cmd := exec.Command("git", "describe", "--always", "--tags", "--dirty")
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output))
	}
	
	// If git describe fails, try just the commit hash
	if Commit != "" {
		return Commit
	}
	
	// Last resort: try git rev-parse HEAD
	cmd = exec.Command("git", "rev-parse", "HEAD")
	output, err = cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output))
	}
	
	return "unknown"
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(getVersion())
	},
}
