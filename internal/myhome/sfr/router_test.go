package sfr

import (
	"context"
	"net"
	"testing"
)

// fakeHost implements model.Host for use in tests.
type fakeHost struct {
	mac  net.HardwareAddr
	ip   net.IP
	name string
}

func (h *fakeHost) Mac() net.HardwareAddr { return h.mac }
func (h *fakeHost) Ip() net.IP            { return h.ip }
func (h *fakeHost) Name() string          { return h.name }
func (h *fakeHost) IsOnline() bool        { return true }
func (h *fakeHost) String() string        { return h.name }

// seedRouter returns a fresh Router pre-seeded with the given host.
func seedRouter(h *fakeHost) *Router {
	r := &Router{}
	r.macs.Store(h.mac.String(), h)
	r.ips.Store(h.ip.String(), h)
	r.names.Store(h.name, h)
	return r
}

func TestGetHostByMac_Found(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	host := &fakeHost{mac: mac, ip: net.ParseIP("10.0.0.1"), name: "device1"}
	r := seedRouter(host)

	got, err := r.GetHostByMac(context.Background(), mac)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "device1" {
		t.Errorf("got name %q, want %q", got.Name(), "device1")
	}
}

func TestGetHostByMac_NotFound(t *testing.T) {
	r := &Router{} // empty
	mac, _ := net.ParseMAC("00:11:22:33:44:55")
	_, err := r.GetHostByMac(context.Background(), mac)
	if err == nil {
		t.Fatal("expected error for missing MAC, got nil")
	}
}

func TestGetHostByIp_Found(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	host := &fakeHost{mac: mac, ip: net.ParseIP("10.0.0.2"), name: "device2"}
	r := seedRouter(host)

	got, err := r.GetHostByIp(context.Background(), net.ParseIP("10.0.0.2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Ip().String() != "10.0.0.2" {
		t.Errorf("got IP %v, want 10.0.0.2", got.Ip())
	}
}

func TestGetHostByIp_NotFound(t *testing.T) {
	r := &Router{}
	_, err := r.GetHostByIp(context.Background(), net.ParseIP("1.2.3.4"))
	if err == nil {
		t.Fatal("expected error for missing IP, got nil")
	}
}

func TestGetHostByName_Found(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	host := &fakeHost{mac: mac, ip: net.ParseIP("10.0.0.3"), name: "my-device"}
	r := seedRouter(host)

	got, err := r.GetHostByName(context.Background(), "my-device")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "my-device" {
		t.Errorf("got name %q, want %q", got.Name(), "my-device")
	}
}

func TestGetHostByName_NotFound(t *testing.T) {
	r := &Router{}
	_, err := r.GetHostByName(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

// TestGetHostByMac_TypeMismatch stores a non-Host value to trigger the type
// assertion fallback path.
func TestGetHostByMac_TypeMismatch(t *testing.T) {
	r := &Router{}
	mac, _ := net.ParseMAC("ff:ff:ff:ff:ff:ff")
	r.macs.Store(mac.String(), "not-a-host")

	_, err := r.GetHostByMac(context.Background(), mac)
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
}

// TestGetHostByIp_TypeMismatch stores a non-Host value to trigger the type
// assertion fallback path.
func TestGetHostByIp_TypeMismatch(t *testing.T) {
	r := &Router{}
	ip := net.ParseIP("9.9.9.9")
	r.ips.Store(ip.String(), 42)

	_, err := r.GetHostByIp(context.Background(), ip)
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
}

// TestGetHostByName_TypeMismatch stores a non-Host value to trigger the type
// assertion fallback path.
func TestGetHostByName_TypeMismatch(t *testing.T) {
	r := &Router{}
	r.names.Store("bad", struct{}{})

	_, err := r.GetHostByName(context.Background(), "bad")
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
}
