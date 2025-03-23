package mynet

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
	mdns "github.com/pion/mdns/v2"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type Resolver interface {
	WithLocalName(hostname string) Resolver
	Start(context.Context) Resolver
	LookupHost(ctx context.Context, host string) (ips []net.IP, err error)
	LookupService(ctx context.Context, service string) (*url.URL, error)
	BrowseService(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error // TODO: use our own type rather than zeroconf.ServiceEntry
}

func MyResolver(log logr.Logger) Resolver {
	theResolverLock.Lock()
	defer theResolverLock.Unlock()

	if theResolver == nil {
		theResolver = &resolver{
			log: log,
		}
	}
	return theResolver
}

type resolver struct {
	sync.Mutex
	localNames []string
	log        logr.Logger
	started    bool
	mdns       *mdns.Conn
	zeroconf   *zeroconf.Resolver
}

var theResolver *resolver

var theResolverLock sync.Mutex

func (r *resolver) WithLocalName(hostname string) Resolver {
	r.Lock()
	defer r.Unlock()

	if r.started {
		r.log.Error(nil, "Resolver already started")
		return theResolver
	}

	r.localNames = append(r.localNames, fmt.Sprintf("%s.local", hostname))
	return theResolver
}

func (r *resolver) Start(ctx context.Context) Resolver {
	r.Lock()
	defer r.Unlock()

	if r.started {
		return r
	}

	iface, ip, err := MainInterface(r.log)
	if err != nil {
		r.log.Error(err, "Unable to find main interface")
		return nil
	}

	zc, err := zeroconf.NewResolver(zeroconf.SelectIPTraffic(zeroconf.IPv4AndIPv6), zeroconf.SelectIfaces([]net.Interface{*iface}))
	if err != nil {
		r.log.Error(err, "Failed to initialize ZeroConf resolver")
		return nil
	}

	r.zeroconf = zc

	addr4, err := net.ResolveUDPAddr("udp4", mdns.DefaultAddressIPv4)
	if err != nil {
		r.log.Error(err, "Unable to resolve mDNS IPv4 UDP address", "address", mdns.DefaultAddressIPv4)
		return nil
	}

	addr6, err := net.ResolveUDPAddr("udp6", mdns.DefaultAddressIPv6)
	if err != nil {
		r.log.Error(err, "Unable to resolve mDNS IPv6 UDP address", "address", mdns.DefaultAddressIPv6)
		return nil
	}

	l4, err := net.ListenUDP("udp4", addr4)
	if err != nil {
		r.log.Error(err, "Unable to listen on mDNS IPv4 UDP address", "address", addr4)
		return nil
	}

	l6, err := net.ListenUDP("udp6", addr6)
	if err != nil {
		r.log.Error(err, "Unable to listen on mDNS IPv6 UDP address", "address", addr6)
		return nil
	}

	go func(ctx context.Context, r *resolver) {
		r.mdns, err = mdns.Server(ipv4.NewPacketConn(l4), ipv6.NewPacketConn(l6), &mdns.Config{
			LocalNames:   r.localNames,
			LocalAddress: *ip,
		})
		if err != nil {
			r.log.Error(err, "unable to publish over mDNS", "hostname", r.localNames)
			panic(err)
		}
		r.log.Info("Published over mDNS", "hostnames", r.localNames, "ip", ip.String())
		r.started = true
		<-ctx.Done()
		r.mdns.Close()
		l4.Close()
		l6.Close()
	}(ctx, r)

	return r
}

func (r *resolver) waitForStart(ctx context.Context) {
	r.Lock()
	for !r.started {
		r.Unlock()
		select {
		case <-ctx.Done():
			r.log.Error(ctx.Err(), "resolver did not start")
			return
		case <-time.After(time.Second):
		}
		r.log.Info("Waiting for resolver to start")
		time.Sleep(100 * time.Millisecond)
		r.Lock()
	}
	r.Unlock()
}

func (r *resolver) LookupHost(ctx context.Context, host string) ([]net.IP, error) {
	r.waitForStart(ctx)

	addrs, err := net.LookupHost(host)
	if err == nil {
		ips := make([]net.IP, len(addrs))
		for i, addr := range addrs {
			ips[i] = net.ParseIP(addr)
		}
		return ips, nil
	}
	_, addr, err := r.mdns.QueryAddr(ctx, fmt.Sprintf("%s.local", host))
	if err != nil {
		return nil, err
	}

	//TODO: query myhome device server

	return []net.IP{addr.AsSlice()}, nil
}

func (r *resolver) LookupService(ctx context.Context, service string) (*url.URL, error) {
	r.waitForStart(ctx)

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}

	entries := make(chan *zeroconf.ServiceEntry)
	instances := make([]*url.URL, 0)

	ctx, cancel := context.WithCancel(ctx)

	go func(ctx context.Context, cancel context.CancelFunc, entries <-chan *zeroconf.ServiceEntry) {
		for {
			select {
			case <-ctx.Done():
				return
			case entry := <-entries:
				// Filter-out spurious candidates
				if strings.Contains(entry.Service, service) {
					for _, addrIpV4 := range entry.AddrIPv4 {
						instances = append(instances, &url.URL{
							Scheme: "tcp",
							Host:   fmt.Sprintf("%v:%v", addrIpV4, entry.Port),
						})
						cancel()
					}
				}
			}
		}
	}(ctx, cancel, entries)

	err = resolver.Browse(ctx, service, "local.", entries)
	if err != nil {
		return nil, err
	}

	<-ctx.Done()

	if len(instances) == 0 {
		return nil, fmt.Errorf("no instance found for service:%s", service)
	} else {
		return instances[0], nil
	}
}

func (r *resolver) BrowseService(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error {
	r.waitForStart(ctx)
	return r.zeroconf.Browse(ctx, service, domain, entries)
}
