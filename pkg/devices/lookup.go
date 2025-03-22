package devices

import (
	"context"
	"net"
	"net/url"
)

type Resolver interface {
	LookupHost(ctx context.Context, host string) (ips []net.IP, err error)
	LookupService(ctx context.Context, service string) (*url.URL, error)
}
