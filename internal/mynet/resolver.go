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
	Start(context.Context) error
	LookupHost(ctx context.Context, host string) (addrs []string, err error)
	LookupService(ctx context.Context, service string) (*url.URL, error)
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

func (r *resolver) Start(ctx context.Context) error {
	r.Lock()
	defer r.Unlock()

	if r.started {
		return nil
	}

	addr4, err := net.ResolveUDPAddr("udp4", mdns.DefaultAddressIPv4)
	if err != nil {
		r.log.Error(err, "Unable to resolve mDNS IPv4 UDP address", "address", mdns.DefaultAddressIPv4)
		return err
	}

	addr6, err := net.ResolveUDPAddr("udp6", mdns.DefaultAddressIPv6)
	if err != nil {
		r.log.Error(err, "Unable to resolve mDNS IPv6 UDP address", "address", mdns.DefaultAddressIPv6)
		return err
	}

	l4, err := net.ListenUDP("udp4", addr4)
	if err != nil {
		r.log.Error(err, "Unable to listen on mDNS IPv4 UDP address", "address", addr4)
		return err
	}

	l6, err := net.ListenUDP("udp6", addr6)
	if err != nil {
		r.log.Error(err, "Unable to listen on mDNS IPv6 UDP address", "address", addr6)
		return err
	}

	_, ip, err := MainInterface(r.log)
	if err != nil {
		r.log.Error(err, "Unable to find main interface")
		return err
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
		<-ctx.Done()
		r.mdns.Close()
		l4.Close()
		l6.Close()
	}(ctx, r)

	r.started = true
	return nil
}

func (r *resolver) waitForStart(ctx context.Context) {
	r.Lock()
	defer r.Unlock()

	for !r.started {

		r.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}

		r.Lock()
	}
}

func (r *resolver) LookupHost(ctx context.Context, host string) (addrs []string, err error) {
	r.waitForStart(ctx)

	ips, err := net.LookupHost(host)
	if err == nil {
		return ips, nil
	}
	_, addr, err := r.mdns.QueryAddr(ctx, fmt.Sprintf("%s.local", host))
	if err != nil {
		return nil, err
	}

	//TODO: query myhome device server

	return []string{addr.String()}, nil
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
