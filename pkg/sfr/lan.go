package sfr

import (
	"encoding/xml"
	"net"
)

type LanHost struct {
	XMLName   xml.Name `xml:"host"`
	Name      string   `xml:"name,attr"`
	Ip        net.IP   `xml:"ip,attr"`
	Mac       string   `xml:"mac,attr"`
	Interface string   `xml:"iface,attr"`
	Probe     uint32   `xml:"probe,attr"`
	Alive     uint32   `xml:"alive,attr"`
	Type      string   `xml:"type,attr"`
	Status    string   `xml:"status,attr"`
}

type DnsHost struct {
	XMLName xml.Name `xml:"dns"`
	Name    string   `xml:"name,attr"`
	Ip      net.IP   `xml:"ip,attr"`
}

func GetHostsList() (*[]*LanHost, error) {
	if len(token) == 0 {
		renewToken()
	}
	params := map[string]string{
		"token": token,
	}
	res, err := queryBox("lan.getHostsList", &params)
	if err != nil {
		log.Info("lan.getHostsList", err)
		return nil, err
	}

	return res.(*[]*LanHost), nil
}

func GetDnsHostList() (*[]*DnsHost, error) {
	if len(token) == 0 {
		renewToken()
	}
	params := map[string]string{
		"token": token,
	}
	res, err := queryBox("lan.getDnsHostList", &params)
	if err != nil {
		log.Info("lan.getDnsHostList", err)
		return nil, err
	}

	return res.(*[]*DnsHost), nil
}
