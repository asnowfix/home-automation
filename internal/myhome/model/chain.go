package model

import (
	"context"
	"net"
)

// chainRouter tries primary first; on any error it falls through to next.
type chainRouter struct {
	primary Router
	next    Router
}

// Chain returns a Router that tries primary first and falls through to next
// on error. Build a priority-ordered lookup stack by nesting calls, e.g.
// Chain(sfrRouter, Chain(mdnsRouter, NotFoundRouter{})).
func Chain(primary, next Router) Router {
	return &chainRouter{primary: primary, next: next}
}

func (r *chainRouter) GetHostByMac(ctx context.Context, mac net.HardwareAddr) (Host, error) {
	host, err := r.primary.GetHostByMac(ctx, mac)
	if err == nil {
		return host, nil
	}
	return r.next.GetHostByMac(ctx, mac)
}

func (r *chainRouter) GetHostByIp(ctx context.Context, ip net.IP) (Host, error) {
	host, err := r.primary.GetHostByIp(ctx, ip)
	if err == nil {
		return host, nil
	}
	return r.next.GetHostByIp(ctx, ip)
}

func (r *chainRouter) GetHostByName(ctx context.Context, name string) (Host, error) {
	host, err := r.primary.GetHostByName(ctx, name)
	if err == nil {
		return host, nil
	}
	return r.next.GetHostByName(ctx, name)
}

// NotFoundRouter is a terminal Router that never has a record for anything.
// Put it at the bottom of a Chain so callers can always treat a Router value
// as non-nil and get a uniform ErrNotFound once every real source has missed.
type NotFoundRouter struct{}

func (NotFoundRouter) GetHostByMac(ctx context.Context, mac net.HardwareAddr) (Host, error) {
	return nil, ErrNotFound
}

func (NotFoundRouter) GetHostByIp(ctx context.Context, ip net.IP) (Host, error) {
	return nil, ErrNotFound
}

func (NotFoundRouter) GetHostByName(ctx context.Context, name string) (Host, error) {
	return nil, ErrNotFound
}
