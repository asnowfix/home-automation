// Package mdnsrouter implements model.Router by resolving a device's mDNS
// (.local) hostname to its current IP. It has no reverse mapping from MAC or
// IP back to a name, so it is meant to be chained after a Router that does
// (e.g. internal/myhome/sfr), via model.Chain.
package mdnsrouter

import (
	"context"
	"fmt"
	"net"

	"github.com/asnowfix/home-automation/internal/myhome/model"
	mynet "github.com/asnowfix/home-automation/internal/myhome/net"

	"github.com/go-logr/logr"
)

// Router resolves hosts by name via DNS/mDNS (see mynet.Resolver.LookupHost).
type Router struct {
	resolver mynet.Resolver
}

// New returns a Router backed by the given resolver (typically mynet.MyResolver(log)).
func New(resolver mynet.Resolver) Router {
	return Router{resolver: resolver}
}

func (Router) GetHostByMac(ctx context.Context, mac net.HardwareAddr) (model.Host, error) {
	return nil, model.ErrNotFound
}

func (Router) GetHostByIp(ctx context.Context, ip net.IP) (model.Host, error) {
	return nil, model.ErrNotFound
}

// GetHostByName resolves name (typically a Shelly device ID, e.g.
// "shellyplus1-a4cf12abcdef") via mynet.Resolver.LookupHost, which tries
// standard DNS first and falls back to mDNS at "<name>.local".
func (r Router) GetHostByName(ctx context.Context, name string) (model.Host, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		log = logr.Discard()
	}

	ips, err := r.resolver.LookupHost(ctx, log, name)
	if err != nil {
		return nil, fmt.Errorf("mDNS lookup of %q failed: %w", name, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("mDNS lookup of %q returned no addresses: %w", name, model.ErrNotFound)
	}

	return &host{name: name, ip: ips[0]}, nil
}

// host is a minimal model.Host backed only by what mDNS resolution can tell
// us: a name and an IP. Mac is unknown to this Router (see GetHostByMac).
type host struct {
	name string
	ip   net.IP
}

func (h *host) Mac() net.HardwareAddr { return nil }
func (h *host) Name() string          { return h.name }
func (h *host) Ip() net.IP            { return h.ip }
func (h *host) IsOnline() bool        { return true }
func (h *host) String() string        { return fmt.Sprintf("%s (%s)", h.name, h.ip) }
