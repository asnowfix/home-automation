package daemon

import (
	"reflect"
	"testing"

	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/spf13/viper"
)

// TestConfigFromFlags_CopiesEveryField guards against a Config field being
// added without being wired in configFromFlags: each Config field except
// Viper must come out equal to options.Flags' field of the same name.
func TestConfigFromFlags_CopiesEveryField(t *testing.T) {
	saved := options.Flags
	t.Cleanup(func() { options.Flags = saved })

	// Give every shared field a distinct non-zero value.
	flags := reflect.ValueOf(&options.Flags).Elem()
	cfgType := reflect.TypeOf(Config{})
	for i := 0; i < cfgType.NumField(); i++ {
		name := cfgType.Field(i).Name
		if name == "Viper" {
			continue
		}
		f := flags.FieldByName(name)
		if !f.IsValid() {
			t.Fatalf("Config.%s has no options.Flags counterpart", name)
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString("v-" + name)
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int, reflect.Int64:
			f.SetInt(int64(i + 1))
		default:
			t.Fatalf("Config.%s: unsupported kind %s", name, f.Kind())
		}
	}

	v := viper.New()
	cfg := configFromFlags(v)
	if cfg.Viper != v {
		t.Errorf("Viper not carried over")
	}
	got := reflect.ValueOf(cfg)
	for i := 0; i < cfgType.NumField(); i++ {
		name := cfgType.Field(i).Name
		if name == "Viper" {
			continue
		}
		if !reflect.DeepEqual(got.Field(i).Interface(), flags.FieldByName(name).Interface()) {
			t.Errorf("Config.%s = %v, want %v", name, got.Field(i).Interface(), flags.FieldByName(name).Interface())
		}
	}
}
