package devices

import (
	"context"
	"net"
	"net/url"

	"github.com/go-logr/logr"
)

type Resolver interface {
	LookupHost(ctx context.Context, log logr.Logger, host string) (ips []net.IP, err error)
	LookupService(ctx context.Context, service string) (*url.URL, error)
}
