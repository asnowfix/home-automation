package daemon

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestIsDaemonCommand verifies daemon-tree detection via the
// IsDaemonAnnotation, including at nesting depths beyond the direct
// "daemon <subcommand>" pattern that a cmd.Parent().Name() == "daemon"
// string check would miss.
func TestIsDaemonCommand(t *testing.T) {
	root := &cobra.Command{Use: "myhome"}
	daemonCmd := &cobra.Command{
		Use: "daemon",
		Annotations: map[string]string{
			IsDaemonAnnotation: "true",
		},
	}
	run := &cobra.Command{Use: "run"}
	nested := &cobra.Command{Use: "nested"}
	deeplyNested := &cobra.Command{Use: "deeper"}
	other := &cobra.Command{Use: "ctl"}
	otherChild := &cobra.Command{Use: "list"}

	root.AddCommand(daemonCmd, other)
	daemonCmd.AddCommand(run)
	run.AddCommand(nested)
	nested.AddCommand(deeplyNested)
	other.AddCommand(otherChild)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"daemon root", daemonCmd, true},
		{"direct child (run)", run, true},
		{"nested two levels deep", nested, true},
		{"nested three levels deep", deeplyNested, true},
		{"sibling command tree (ctl)", other, false},
		{"child of sibling tree (ctl list)", otherChild, false},
		{"unattached command", &cobra.Command{Use: "detached"}, false},
		{"nil command", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDaemonCommand(tt.cmd); got != tt.want {
				t.Errorf("IsDaemonCommand(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}

	// Sanity: the real daemon.Cmd tree carries the annotation and covers
	// both existing subcommands, which is what myhome/main.go relies on to
	// pick daemon-vs-ctl logging defaults.
	if !IsDaemonCommand(runCmd) {
		t.Errorf("expected the real runCmd to be recognized as a daemon command")
	}
	if !IsDaemonCommand(installCmd) {
		t.Errorf("expected the real installCmd to be recognized as a daemon command")
	}
	if !IsDaemonCommand(uninstallCmd) {
		t.Errorf("expected the real uninstallCmd to be recognized as a daemon command")
	}
}
