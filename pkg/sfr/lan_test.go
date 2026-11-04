package sfr

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
)

// setBoxIp injects a test IP into the package-level boxIp cache and restores
// the original value via t.Cleanup. This prevents getBoxIp from attempting
// live gateway discovery during unit tests.
func setBoxIp(t *testing.T, ip net.IP) {
	t.Helper()
	boxIpMutex.Lock()
	saved := boxIp
	boxIp = ip
	boxIpMutex.Unlock()
	t.Cleanup(func() {
		boxIpMutex.Lock()
		boxIp = saved
		boxIpMutex.Unlock()
	})
}

func TestGetLanInfo(t *testing.T) {
	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rsp stat="ok" version="1.0"><lan ip_addr="192.168.1.1" netmask="255.255.255.0" dhcp_active="on" dhcp_start="192.168.1.20" dhcp_end="192.168.1.100" dhcp_lease="86400"/></rsp>`)
	})

	info, err := GetLanInfo(context.Background(), ip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Ip.String() != "192.168.1.1" {
		t.Errorf("LanInfo.Ip = %s, want 192.168.1.1", info.Ip)
	}
	if info.DhcpActive != "on" {
		t.Errorf("DhcpActive = %q, want on", info.DhcpActive)
	}
}

func TestGetHostsList(t *testing.T) {
	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rsp stat="ok" version="1.0"><host name="mypc" ip="192.168.1.100" mac="aa:bb:cc:dd:ee:ff" iface="eth0" probe="1" alive="1" type="pc" status="online"/></rsp>`)
	})
	setBoxIp(t, ip)

	hosts, err := GetHostsList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(*hosts))
	}
	if (*hosts)[0].Name != "mypc" {
		t.Errorf("host name = %q, want mypc", (*hosts)[0].Name)
	}
	if (*hosts)[0].Status != "online" {
		t.Errorf("host status = %q, want online", (*hosts)[0].Status)
	}
}

func TestGetHostsListNoBoxIP(t *testing.T) {
	setBoxIp(t, nil)

	_, err := GetHostsList(context.Background())
	if err == nil {
		t.Fatal("expected error when no box IP is available, got nil")
	}
}

func TestGetDnsHostList(t *testing.T) {
	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rsp stat="ok" version="1.0"><dns name="myserver" ip="192.168.1.50"/></rsp>`)
	})
	setBoxIp(t, ip)

	hosts, err := GetDnsHostList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*hosts) != 1 {
		t.Fatalf("expected 1 DNS host, got %d", len(*hosts))
	}
	if (*hosts)[0].Name != "myserver" {
		t.Errorf("dns host name = %q, want myserver", (*hosts)[0].Name)
	}
}

func TestGetDnsHostListNoBoxIP(t *testing.T) {
	setBoxIp(t, nil)

	_, err := GetDnsHostList(context.Background())
	if err == nil {
		t.Fatal("expected error when no box IP is available, got nil")
	}
}
