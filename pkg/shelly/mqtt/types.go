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

// ChannelMqtt

type Event struct {
	Src    string                 `json:"src"`    // Source of the event (Device Id)
	Dst    string                 `json:"dst"`    // Destination of the event (MQTT topic)
	Method string                 `json:"method"` // One of NotifyStatus, NotifyEvent, NotifyFullStatus
	Params map[string]interface{} `json:"params"` // Parameters of the event
	// Params struct {
	// 	Timestamp float64              `json:"ts"`
	// 	BLE       *any                 `json:"ble"`
	// 	BtHome    *any                 `json:"bthome"`
	// 	Cloud     *any                 `json:"cloud"`
	// 	Input0    *sswitch.InputStatus `json:"input:0"`
	// 	Input1    *sswitch.InputStatus `json:"input:1"`
	// 	Input2    *sswitch.InputStatus `json:"input:2"`
	// 	Input3    *sswitch.InputStatus `json:"input:3"`
	// 	Knx       *any                 `json:"knx"`
	// 	Mqtt      *Status              `json:"mqtt"`
	// 	Switch0   *sswitch.Status      `json:"switch:0"`
	// 	Switch1   *sswitch.Status      `json:"switch:1"`
	// 	Switch2   *sswitch.Status      `json:"switch:2"`
	// 	Switch3   *sswitch.Status      `json:"switch:3"`
	// 	System    *system.Status       `json:"system"`
	// 	Wifi      *wifi.StatusEvent    `json:"wifi"`
	// 	WebSocket *any                 `json:"ws"`
	//  Events    *[]ComponentEvent    `json:"events"`
	// } `json:"params"` // Parameters of the event
}

type ComponentEvent struct {
	Component       string  `json:"component"`
	Id              int     `json:"id"`
	Event           string  `json:"event"`
	RestartRequired bool    `json:"restart_required"`
	Ts              float64 `json:"ts"`
	CfgRev          int     `json:"cfg_rev"`
}

type Dialog struct {
	Id  uint   `json:"id"`
	Src string `json:"src"`
}

type Request struct {
	Dialog
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type Response struct {
	Dialog
	Dst    string `json:"dst"`
	Result *any   `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
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
