package mqtt

type Qos uint

const (
	AtMostOnce Qos = iota
	AtLeastOnce
	ExactlyOnce
)

func (qos Qos) String() string {
	return [...]string{
		"AtMostOnce",
		"AtLeastOnce",
		"ExactlyOnce",
	}[qos]
}

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt/

type SslCa uint

const (
	NoSsl          SslCa = iota // Plain TCP connection
	SkipValidation              // TLS with disabled certificate validation
	UserCa                      // TLS connection verified by the user-provided CA
	BuiltinCa                   // TLS connection verified by the built-in CA bundle
)

// The configuration of the MQTT component contains information about the credentials and prefix used and the protection and notifications settings of the MQTT connection.
type Configuration struct {
	Enable         bool   `json:"enable"`                 // True if MQTT connection is enabled, false otherwise
	Server         string `json:"server,omitempty"`       // Host name of the MQTT server. Can be followed by port number - host:port
	ClientId       string `json:"client_id,omitempty"`    // Identifies each MQTT client that connects to an MQTT brokers (when null, Device id is used as client_id)
	User           string `json:"user,omitempty"`         // Username
	SslCa          SslCa  `json:"ssl_ca,omitempty"`       // Type of the TCP sockets
	TopicPrefic    string `json:"topic_prefix,omitempty"` // Prefix of the topics on which device publish/subscribe. Limited to 300 characters. Could not start with $ and #, +, %, ? are not allowed. (when null, Device id is used as topic prefix)
	RpcNotifs      bool   `json:"rpc_ntf"`                // Enables RPC notifications (NotifyStatus and NotifyEvent) to be published on <device_id|topic_prefix>/events/rpc (<topic_prefix> when a custom prefix is set, <device_id> otherwise). Default value: true.
	StatusNotifs   bool   `json:"status_ntf"`             // Enables publishing the complete component status on <device_id|topic_prefix>/status/<component>:<id> (<topic_prefix> when a custom prefix is set, <device_id> otherwise). The complete status will be published if a signifficant change occurred. Default value: false
	UseClientCerts bool   `json:"use_client_cert"`        // Enable or diable usage of client certifactes to use MQTT with encription, default: false
	EnableControl  bool   `json:"enable_control"`         // Enable the MQTT control feature. Defalut value: true
}

type Status struct {
	Connected bool `json:"connected"` // True if the device is MQTT connected, false otherwise
}

type ConfigResults struct {
	Id     uint   `json:"id"`
	Source string `json:"src"`
	Result struct {
		RestartRequired bool `json:"restart_required"`
	}
}
