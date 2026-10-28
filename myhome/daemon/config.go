package daemon

import (
	"time"

	"github.com/spf13/viper"
)

// Config is everything the daemon needs to run, resolved once at the
// composition root (runCmd in run.go: flags, then config file, then
// defaults) and handed to NewDaemon. The daemon and the services it wires
// read this struct only; they never reach for myhome/ctl/options' CLI
// globals (#362).
type Config struct {
	InstanceName                string
	MqttBroker                  string
	MdnsTimeout                 time.Duration
	MqttTimeout                 time.Duration
	MqttGrace                   time.Duration
	RefreshInterval             time.Duration
	MqttWatchdogInterval        time.Duration
	MqttWatchdogMaxFailures     int
	MqttReconnectInterval       time.Duration
	MqttBrokerClientLogInterval time.Duration
	EventsDir                   string
	UiPort                      int
	EnableGen1Proxy             bool
	EnableOccupancyService      bool
	EnableTemperatureService    bool
	EnableMetricsExporter       bool
	MetricsExporterPort         int
	MetricsExporterTopic        string
	ShellyRateLimit             time.Duration
	AutoSetup                   bool
	ReconcileInterval           time.Duration
	NoMdnsPublish               bool
	EnableEventsService         bool
	EventsDBPath                string
	EventsRetention             time.Duration
	RemoteProxy                 string
	PoolDeviceID                string
	PoolEnabled                 bool
	BeemEmail                   string
	BeemPassword                string
	BeemPollInterval            time.Duration
	SolarStaleAfter             time.Duration
	SFRUsername                 string
	SFRPassword                 string
	EnableNoticeService         bool
	NoticeNightStart            string
	NoticeNightEnd              string
	NoticeDigestHour            int
	SMTPHost                    string
	SMTPPort                    int
	SMTPUsername                string
	SMTPPassword                string
	SMTPFrom                    string
	SMTPTo                      string

	// Viper is the loaded config file/environment, for the few options
	// resolved lazily by lower layers (e.g. the embedded broker). May be nil.
	Viper *viper.Viper
}
