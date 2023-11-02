package shelly

import "net"

type Device struct {
	Host        string `json:"host"`
	Ip          net.IP
	Model       string `json:"model"`
	Serial      string `json:"serial"`
	Application string `json:"app"`
	Version     string `json:"ver"`
	Generation  int    `json:"gen"`
}
