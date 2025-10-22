package daemon

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(installCmd)
	Cmd.AddCommand(uninstallCmd)
}

func load(ctx context.Context) (service.Service, service.Logger, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, nil, err
	}

	// // Initialize viper
	// viper.SetConfigName("myhome") // name of config file (without extension)
	// viper.SetConfigType("yaml")   // or viper.SetConfigType("toml")
	// viper.AddConfigPath(".")      // optionally look for config in the working directory
	// err := viper.ReadInConfig()   // Find and read the config file
	// if err != nil {
	// 	log.Error(err, "Error reading config file")
	// 	disableEmbeddedMqttBroker = false
	// 	disableDeviceManager = true
	// } else {
	// 	// Read the configuration option to disable MQTT broker startup
	// 	disableEmbeddedMqttBroker = viper.GetBool("disable_embedded_mqtt")
	// 	disableDeviceManager = viper.GetBool("disable_device_manager")
	// }
	config := service.Config{
		Name:        "myhome",
		DisplayName: "MyHome",
		Description: "MyHome Daemon, with embedded MQTT broker and persistent device manager",
	}

	daemon := NewDaemon(ctx)

	s, err := service.New(daemon, &config)
	if err != nil {
		log.Error(err, "Failed to create (background) service")
		return nil, nil, err
	}
	logger, err := s.Logger(nil)
	if err != nil {
		log.Error(err, "Failed to create (background) service")
		return nil, nil, err
	}
	return s, logger, err
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install MyHome as a " + service.Platform() + " service",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, l, err := load(cmd.Context())
		if err != nil {
			return err
		}
		l.Info("Installing service")
		return s.Install()
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall MyHome as a " + service.Platform() + " service",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, l, err := load(cmd.Context())
		if err != nil {
			return err
		}
		l.Info("Uninstalling service")
		return s.Uninstall()
	},
}
