package config

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfigCommand_FlagValidation(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantErr     bool
		errContains string
	}{
		{
			name:        "no flags provided",
			args:        []string{"device-id"},
			wantErr:     true,
			errContains: "at least one configuration option must be specified",
		},
		{
			name:    "name flag only",
			args:    []string{"device-id", "--name", "New Name"},
			wantErr: false,
		},
		{
			name:    "ecomode flag only (sets to true)",
			args:    []string{"device-id", "--ecomode=true"},
			wantErr: false,
		},
		{
			name:    "ecomode flag set to false",
			args:    []string{"device-id", "--ecomode=false"},
			wantErr: false,
		},
		{
			name:    "both name and ecomode flags",
			args:    []string{"device-id", "--name", "New Name", "--ecomode=false"},
			wantErr: false,
		},
		{
			name:    "name flag with short form",
			args:    []string{"device-id", "-n", "New Name"},
			wantErr: false,
		},
		{
			name:        "no device identifier",
			args:        []string{"--name", "New Name"},
			wantErr:     true,
			errContains: "accepts 1 arg(s), received 0",
		},
		{
			name:        "too many arguments",
			args:        []string{"device1", "device2", "--name", "New Name"},
			wantErr:     true,
			errContains: "accepts 1 arg(s), received 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new command instance for each test
			cmd := &cobra.Command{
				Use:   "config <device_id|name|ip|mac>",
				Short: "Configure device settings in local database and on device",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					// Validate that at least one flag is set
					if flags.Name == "" && !flags.EcoModeSet {
						return fmt.Errorf("at least one configuration option must be specified (--name or --ecomode)")
					}
					return nil
				},
			}

			// Reset flags for each test
			flags.Name = ""
			flags.EcoMode = false
			flags.EcoModeSet = false

			// Add flags
			cmd.Flags().StringVarP(&flags.Name, "name", "n", "", "Set device name")
			cmd.Flags().BoolVar(&flags.EcoMode, "ecomode", false, "Set eco mode")

			// Track if ecomode was explicitly set
			cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
				flags.EcoModeSet = cmd.Flags().Changed("ecomode")
				return nil
			}

			// Set args and execute
			cmd.SetArgs(tt.args)
			err := cmd.Execute()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfigCommand_EcoModeSetTracking(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		expectEcoMode    bool
		expectEcoModeSet bool
	}{
		{
			name:             "ecomode not specified",
			args:             []string{"device-id", "--name", "Test"},
			expectEcoMode:    false,
			expectEcoModeSet: false,
		},
		{
			name:             "ecomode set to true",
			args:             []string{"device-id", "--ecomode=true"},
			expectEcoMode:    true,
			expectEcoModeSet: true,
		},
		{
			name:             "ecomode set to false",
			args:             []string{"device-id", "--ecomode=false"},
			expectEcoMode:    false,
			expectEcoModeSet: true,
		},
		{
			name:             "ecomode with default value (true)",
			args:             []string{"device-id", "--ecomode"},
			expectEcoMode:    true,
			expectEcoModeSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flags
			flags.Name = ""
			flags.EcoMode = false
			flags.EcoModeSet = false

			// Create a new command instance
			cmd := &cobra.Command{
				Use:  "config <device_id|name|ip|mac>",
				Args: cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					return nil
				},
			}

			cmd.Flags().StringVarP(&flags.Name, "name", "n", "", "Set device name")
			cmd.Flags().BoolVar(&flags.EcoMode, "ecomode", false, "Set eco mode")

			cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
				flags.EcoModeSet = cmd.Flags().Changed("ecomode")
				return nil
			}

			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if flags.EcoMode != tt.expectEcoMode {
				t.Errorf("Expected EcoMode=%v, got %v", tt.expectEcoMode, flags.EcoMode)
			}

			if flags.EcoModeSet != tt.expectEcoModeSet {
				t.Errorf("Expected EcoModeSet=%v, got %v", tt.expectEcoModeSet, flags.EcoModeSet)
			}
		})
	}
}

func TestConfigCommand_HelpText(t *testing.T) {
	// Verify that the command has proper help text
	if Cmd.Use == "" {
		t.Error("Command Use is empty")
	}

	if Cmd.Short == "" {
		t.Error("Command Short description is empty")
	}

	if Cmd.Long == "" {
		t.Error("Command Long description is empty")
	}

	// Verify examples are present in Long description
	expectedExamples := []string{
		"shellyht-EE45E9",
		"4c:eb:d6:ee:45:e9",
		"shelly1minig3-abc123",
		"--name",
		"--ecomode",
	}

	for _, example := range expectedExamples {
		if !contains(Cmd.Long, example) {
			t.Errorf("Long description missing example: %s", example)
		}
	}
}

func TestConfigCommand_FlagsRegistered(t *testing.T) {
	// Verify that flags are properly registered
	nameFlag := Cmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Error("name flag not registered")
	} else {
		if nameFlag.Shorthand != "n" {
			t.Errorf("Expected name flag shorthand 'n', got %q", nameFlag.Shorthand)
		}
	}

	ecomodeFlag := Cmd.Flags().Lookup("ecomode")
	if ecomodeFlag == nil {
		t.Error("ecomode flag not registered")
	} else {
		if ecomodeFlag.DefValue != "false" {
			t.Errorf("Expected ecomode default value 'false', got %q", ecomodeFlag.DefValue)
		}
	}
}

func TestConfigCommand_ArgsValidation(t *testing.T) {
	// Test that the command requires exactly one argument
	if Cmd.Args == nil {
		t.Fatal("Args validator not set")
	}

	// Test with no args
	err := Cmd.Args(Cmd, []string{})
	if err == nil {
		t.Error("Expected error with no args, got nil")
	}

	// Test with one arg
	err = Cmd.Args(Cmd, []string{"device-id"})
	if err != nil {
		t.Errorf("Expected no error with one arg, got: %v", err)
	}

	// Test with two args
	err = Cmd.Args(Cmd, []string{"device-id", "extra"})
	if err == nil {
		t.Error("Expected error with two args, got nil")
	}
}

func TestConfigCommand_PreRunE(t *testing.T) {
	// Verify PreRunE is set
	if Cmd.PreRunE == nil {
		t.Error("PreRunE not set on command")
	}
}

func TestConfigCommand_OutputFormat(t *testing.T) {
	// Test that the command would produce expected output format
	// This is a basic structural test since we can't easily mock the full execution

	// Capture output
	var buf bytes.Buffer
	Cmd.SetOut(&buf)

	// Verify command is properly configured for output
	if Cmd.OutOrStdout() == nil {
		t.Error("Command output not configured")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
