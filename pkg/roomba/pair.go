package roomba

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// DefaultPairTimeout bounds Pair when ctx has no deadline of its own.
const DefaultPairTimeout = 20 * time.Second

// Scanner lists the robots answering discovery; *Discoverer implements it.
type Scanner interface {
	Scan(ctx context.Context) ([]Info, error)
}

// Pairer finds a robot whose HOME button was just held and fetches its local
// password. The zero value uses broadcast discovery and GetPassword; the fields
// exist so tests can replace the network.
type Pairer struct {
	// Scanner finds the candidate. Nil: broadcast discovery, or a unicast probe
	// to the host given to Pair.
	Scanner Scanner
	// Password runs the password handshake. Nil: GetPassword.
	Password func(ctx context.Context, host, blid string) (string, error)
}

// Pair is (&Pairer{}).Pair.
func Pair(ctx context.Context, host string) (Info, string, error) {
	return (&Pairer{}).Pair(ctx, host)
}

// Pair finds the robot to pair (the one at host, or the only robot answering
// discovery when host is empty) and returns its identity and local password.
// The password is returned to the caller only: Pair neither logs nor stores it.
func (p *Pairer) Pair(ctx context.Context, host string) (Info, string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultPairTimeout)
		defer cancel()
	}
	info, err := p.candidate(ctx, host)
	if err != nil {
		return Info{}, "", err
	}
	get := p.Password
	if get == nil {
		get = GetPassword
	}
	for {
		pctx, pcancel := context.WithTimeout(ctx, 4*time.Second)
		password, err := get(pctx, info.IP, info.BLID)
		pcancel()
		if err == nil {
			return info, password, nil
		}
		if errors.Is(err, ErrNeedsButton) || ctx.Err() != nil {
			return Info{}, "", err
		}
		// The robot refuses a connection that follows another too closely: retry.
		select {
		case <-ctx.Done():
			return Info{}, "", err
		case <-time.After(time.Second):
		}
	}
}

// candidate finds the robot to pair: the one at host, or the only robot on the
// LAN that answers discovery.
func (p *Pairer) candidate(ctx context.Context, host string) (Info, error) {
	s := p.Scanner
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil {
			return Info{}, fmt.Errorf("host must be an IP address, got %q", host)
		}
		if s == nil {
			s = &Discoverer{Addr: &net.UDPAddr{IP: ip, Port: DiscoveryPort}}
		}
	}
	if s == nil {
		s = &Discoverer{}
	}
	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	found, err := s.Scan(sctx)
	if err != nil {
		return Info{}, err
	}
	switch len(found) {
	case 0:
		if host != "" {
			return Info{}, fmt.Errorf("no robot answered discovery at %s", host)
		}
		return Info{}, errors.New("no robot answered discovery; retry, or pass its IP address")
	case 1:
		return found[0], nil
	}
	names := make([]string, len(found))
	for i, f := range found {
		names[i] = f.Name + " (" + f.IP + ")"
	}
	return Info{}, fmt.Errorf("several robots answered (%s): pass the IP address of the one to pair", strings.Join(names, ", "))
}
