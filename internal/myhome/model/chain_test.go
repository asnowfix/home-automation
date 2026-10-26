package model

import (
	"context"
	"errors"
	"net"
	"testing"
)

// fakeHost implements Host for use in tests.
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

// stubRouter returns host/err regardless of the key asked for.
type stubRouter struct {
	host Host
	err  error
}

func (s stubRouter) GetHostByMac(ctx context.Context, mac net.HardwareAddr) (Host, error) {
	return s.host, s.err
}

func (s stubRouter) GetHostByIp(ctx context.Context, ip net.IP) (Host, error) {
	return s.host, s.err
}

func (s stubRouter) GetHostByName(ctx context.Context, name string) (Host, error) {
	return s.host, s.err
}

func TestChain_PrimaryFound(t *testing.T) {
	want := &fakeHost{name: "primary-host"}
	primary := stubRouter{host: want}
	next := stubRouter{err: errors.New("should not be called")}

	r := Chain(primary, next)

	got, err := r.GetHostByName(context.Background(), "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestChain_FallsThroughOnError(t *testing.T) {
	want := &fakeHost{name: "fallback-host"}
	primary := stubRouter{err: ErrNotFound}
	next := stubRouter{host: want}

	r := Chain(primary, next)

	got, err := r.GetHostByMac(context.Background(), net.HardwareAddr{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestChain_AllMissTerminatesAtNotFoundRouter(t *testing.T) {
	primary := stubRouter{err: ErrNotFound}
	middle := stubRouter{err: ErrNotFound}

	r := Chain(primary, Chain(middle, NotFoundRouter{}))

	_, err := r.GetHostByIp(context.Background(), net.ParseIP("10.0.0.1"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNotFoundRouter_AlwaysErrNotFound(t *testing.T) {
	var r Router = NotFoundRouter{}

	if _, err := r.GetHostByMac(context.Background(), net.HardwareAddr{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetHostByMac: expected ErrNotFound, got %v", err)
	}
	if _, err := r.GetHostByIp(context.Background(), net.IP{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetHostByIp: expected ErrNotFound, got %v", err)
	}
	if _, err := r.GetHostByName(context.Background(), "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetHostByName: expected ErrNotFound, got %v", err)
	}
}
