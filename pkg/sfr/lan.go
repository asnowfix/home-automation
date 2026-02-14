package sfr

import (
	"context"
	"encoding/xml"
	"net"
)

// <?xml version="1.0" encoding="UTF-8"?>
// <rsp stat="ok" version="1.0">
//
//	<lan ip_addr="192.168.1.1" netmask="255.255.255.0" dhcp_active="on" dhcp_start="192.168.1.20" dhcp_end="192.168.1.100" dhcp_lease="86400" />
//
// </rsp>
type LanInfo struct {
	XMLName    xml.Name `xml:"lan"`
	Ip         net.IP   `xml:"ip_addr,attr"`
	NetMask    net.IP   `xml:"netmask,attr"`
	DhcpActive string   `xml:"dhcp_active,attr"`
	DhcpStart  net.IP   `xml:"dhcp_start,attr"`
	DhcpEnd    net.IP   `xml:"dhcp_end,attr"`
	DhcpLease  uint32   `xml:"dhcp_lease,attr"`
}

func GetLanInfo(ctx context.Context, ip net.IP) (*LanInfo, error) {
	params := map[string]string{
		// "token": token, // no more needed: public IRL
	}
	res, err := queryBox(ip, "lan.getInfo", &params)
	if err != nil {
		log.Error(err, "lan.getInfo")
		return nil, err
	}

	return res.(*LanInfo), nil
}

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

func GetHostsList(ctx context.Context) (*[]*LanHost, error) {
	ip := getBoxIp(ctx)
	// if len(token) == 0 {
	// 	renewToken(ip)
	// }
	params := map[string]string{
		// "token": token, // no more needed: public IRL
	}
	res, err := queryBox(ip, "lan.getHostsList", &params)
	if err != nil {
		log.Error(err, "lan.getHostsList")
		return nil, err
	}

	return res.(*[]*LanHost), nil
}

func GetDnsHostList(ctx context.Context) (*[]*DnsHost, error) {
	ip := getBoxIp(ctx)
	// if len(token) == 0 {
	// 	renewToken(ip)
	// }
	params := map[string]string{
		// "token": token, // no more needed: public IRL
	}
	res, err := queryBox(ip, "lan.getDnsHostList", &params)
	if err != nil {
		log.Error(err, "lan.getDnsHostList")
		return nil, err
	}

	return res.(*[]*DnsHost), nil
}
