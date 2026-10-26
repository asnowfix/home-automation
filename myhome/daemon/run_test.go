package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/spf13/viper"
)

// TestLoadConfigFileMissing verifies that a completely absent config file is
// not an error: defaults/flags/env are expected to take over (this is the
// documented "config file is optional" behavior).
func TestLoadConfigFileMissing(t *testing.T) {
	v := viper.New()
	v.SetConfigName("myhome")
	v.SetConfigType("yaml")
	v.AddConfigPath(t.TempDir()) // empty directory: no myhome.yaml present

	if err := loadConfigFile(v, logr.Discard()); err != nil {
		t.Fatalf("loadConfigFile with no config file present: got error %v, want nil", err)
	}
}

// TestLoadConfigFileValid verifies a well-formed config file loads without
// error and its values are readable afterward.
func TestLoadConfigFileValid(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "pool:\n  device_id: \"shellypro1-abc123\"\n  enabled: true\n")

	v := viper.New()
	v.SetConfigName("myhome")
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)

	if err := loadConfigFile(v, logr.Discard()); err != nil {
		t.Fatalf("loadConfigFile with valid config: got error %v, want nil", err)
	}
	if got := v.GetString("pool.device_id"); got != "shellypro1-abc123" {
		t.Errorf("pool.device_id = %q, want %q", got, "shellypro1-abc123")
	}
	if !v.GetBool("pool.enabled") {
		t.Errorf("pool.enabled = false, want true")
	}
}

// TestLoadConfigFileInvalid verifies that a config file which exists but
// fails to parse — the exact class of bug found in production, where
// myhome.yaml's header comment was missing its leading '#' and the whole
// file silently failed to load for weeks — now aborts startup with an error
// instead of being swallowed under the same message as "no config file".
func TestLoadConfigFileInvalid(t *testing.T) {
	dir := t.TempDir()
	// Reproduces the real production bug: an unprefixed comment line collides
	// with the following mapping key, making the document invalid YAML.
	writeConfig(t, dir, "#\n MyHome Configuration Example\npool:\n  enabled: true\n")

	v := viper.New()
	v.SetConfigName("myhome")
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)

	err := loadConfigFile(v, logr.Discard())
	if err == nil {
		t.Fatal("loadConfigFile with malformed config: got nil error, want non-nil")
	}
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, "myhome.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test config: %v", err)
	}
}
