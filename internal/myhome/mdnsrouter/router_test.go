package mdnsrouter

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome/model"
	mynet "github.com/asnowfix/home-automation/internal/myhome/net"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

// fakeResolver implements mynet.Resolver with a canned LookupHost result;
// the other methods are unused by mdnsrouter and just satisfy the interface.
type fakeResolver struct {
	ips []net.IP
	err error
}

func (f *fakeResolver) WithLocalName(ctx context.Context, hostname string) mynet.Resolver { return f }
func (f *fakeResolver) LookupHost(ctx context.Context, log logr.Logger, host string) ([]net.IP, error) {
	return f.ips, f.err
}
func (f *fakeResolver) LookupService(ctx context.Context, service string) (*url.URL, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeResolver) BrowseService(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error {
	return errors.New("not implemented")
}
func (f *fakeResolver) PublishService(ctx context.Context, instance, service, domain string, port int, txt []string, ifaces []net.Interface) (*zeroconf.Server, error) {
	return nil, errors.New("not implemented")
}

func TestGetHostByName_Found(t *testing.T) {
	r := New(&fakeResolver{ips: []net.IP{net.ParseIP("192.168.1.42")}})

	host, err := r.GetHostByName(context.Background(), "shellyplus1-a4cf12abcdef")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host.Ip().String() != "192.168.1.42" {
		t.Errorf("got IP %v, want 192.168.1.42", host.Ip())
	}
	if host.Name() != "shellyplus1-a4cf12abcdef" {
		t.Errorf("got name %q", host.Name())
	}
}

func TestGetHostByName_LookupError(t *testing.T) {
	r := New(&fakeResolver{err: errors.New("no such host")})

	_, err := r.GetHostByName(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetHostByName_NoAddresses(t *testing.T) {
	r := New(&fakeResolver{ips: nil})

	_, err := r.GetHostByName(context.Background(), "ghost")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetHostByMac_Unsupported(t *testing.T) {
	r := New(&fakeResolver{})

	_, err := r.GetHostByMac(context.Background(), net.HardwareAddr{})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetHostByIp_Unsupported(t *testing.T) {
	r := New(&fakeResolver{})

	_, err := r.GetHostByIp(context.Background(), net.IP{})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
