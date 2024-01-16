package mqtt

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt/

type SslCa uint

const (
	NoSsl SslCa = iota
	SkipValidation
	UserCa
	BuiltinCa
)

type Configuration struct {
	Enable         bool   `json:"enable"`
	Server         string `json:"server,omitempty"`
	ClientId       string `json:"client_id,omitempty"`
	User           string `json:"user,omitempty"`
	SslCa          SslCa  `json:"ssl_ca,omitempty"`
	TopicPrefic    string `json:"topic_prefix,omitempty"`
	RpcNotifs      bool   `json:"rpc_ntf"`
	StatusNotifs   bool   `json:"status_ntf"`
	UseClientCerts bool   `json:"use_client_cert"`
	EnableControl  bool   `json:"enable_control"`
}

type Status struct {
	Connected bool `json:"connected"`
}
