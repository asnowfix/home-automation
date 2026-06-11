package myhome

// Script-host RPC types: devices (or other daemons/CLI) invoke handlers
// exposed by daemon-hosted scripts via MyHome.on(name, fn).

// ScriptInvokeParams represents parameters for script.invoke
type ScriptInvokeParams struct {
	Script string `json:"script"`           // script name, with or without the .js suffix
	Name   string `json:"name"`             // handler name registered by the script via MyHome.on()
	Params any    `json:"params,omitempty"` // free-form JSON forwarded to the handler
}

// ScriptInvokeResult represents the result of script.invoke
type ScriptInvokeResult struct {
	Result any `json:"result,omitempty"` // value returned by the script handler
}

// LanHostInfo describes one host seen on the LAN (infrastructure data used by
// the occupancy workflow).
type LanHostInfo struct {
	Name   string `json:"name"`
	Ip     string `json:"ip"`
	Mac    string `json:"mac"`
	Status string `json:"status"`
	Alive  uint32 `json:"alive"`
}

// LanHostsResult represents the result of lan.hosts
type LanHostsResult struct {
	Hosts []LanHostInfo `json:"hosts"`
}
