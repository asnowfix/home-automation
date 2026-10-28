package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/myhome/ctl/options"
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

// TestVacuumConfigFromEnv verifies the robot vacuum follows the Beem/SFR
// pattern (#626): MYHOME_VACUUM_* environment variables (the packaged daemon's
// .env) reach the vacuum service, which is enabled only when both the BLID and
// the password are set.
func TestVacuumConfigFromEnv(t *testing.T) {
	saved := options.Flags
	t.Cleanup(func() { options.Flags = saved })

	newViper := func() *viper.Viper {
		v := viper.New()
		v.SetEnvPrefix("MYHOME")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()
		return v
	}

	for _, env := range []map[string]string{
		{},
		{"MYHOME_VACUUM_BLID": "0123456789ABCDEF"},
		{"MYHOME_VACUUM_PASSWORD": "test-password"},
	} {
		for _, k := range []string{"MYHOME_VACUUM_BLID", "MYHOME_VACUUM_PASSWORD", "MYHOME_VACUUM_HOST"} {
			t.Setenv(k, env[k])
		}
		readVacuumConfig(newViper())
		if vacuumConfig().Enabled() {
			t.Fatalf("enabled with only %v set", keys(env))
		}
	}

	t.Setenv("MYHOME_VACUUM_BLID", "0123456789ABCDEF")
	t.Setenv("MYHOME_VACUUM_PASSWORD", "test-password")
	t.Setenv("MYHOME_VACUUM_HOST", "192.0.2.50")
	readVacuumConfig(newViper())
	cfg := vacuumConfig()
	if !cfg.Enabled() {
		t.Fatal("disabled with MYHOME_VACUUM_BLID and MYHOME_VACUUM_PASSWORD set")
	}
	if cfg.BLID != "0123456789ABCDEF" || cfg.Password != "test-password" || cfg.Host != "192.0.2.50" {
		t.Fatalf("config not read from the environment: blid=%q host=%q", cfg.BLID, cfg.Host)
	}
}

func keys(m map[string]string) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
