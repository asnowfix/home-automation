// Package roomba speaks the local (cloud-independent) control protocol of iRobot
// Roomba vacuums, as reverse-engineered by dorita980
// (https://github.com/koalazak/dorita980): UDP discovery, the one-time password
// handshake, and MQTT over TLS to the broker the robot runs on port 8883.
//
// Field facts the code relies on are measured on a Roomba i7 and recorded in
// docs/vacuum-plan.md and issue #606.
package roomba

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"
)

// DiscoveryPort is the UDP port robots listen on for the discovery probe.
const DiscoveryPort = 5678

// discoveryProbe is the payload robots answer to.
var discoveryProbe = []byte("irobotmcs")

// ErrNotFound is returned by Find when no robot with the requested BLID answered.
var ErrNotFound = errors.New("roomba: no robot with this BLID answered discovery")

// Info is a robot's discovery reply. None of it is secret.
type Info struct {
	BLID     string `json:"-"`
	Hostname string `json:"hostname"` // "Roomba-<BLID>" or "iRobot-<BLID>"
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Name     string `json:"robotname"`
	SKU      string `json:"sku"`
	Firmware string `json:"sw"`
}

// parseInfo decodes one discovery reply; ok is false for anything that is not a robot.
func parseInfo(b []byte) (Info, bool) {
	var info Info
	if json.Unmarshal(b, &info) != nil || info.IP == "" {
		return Info{}, false
	}
	prefix, blid, found := strings.Cut(info.Hostname, "-")
	if !found || (prefix != "Roomba" && prefix != "iRobot") || blid == "" {
		return Info{}, false
	}
	info.BLID = blid
	return info, true
}

// Discoverer broadcasts the discovery probe. The zero value is ready to use.
//
// A single probe can go unanswered: live measurements saw 1 miss in 3 runs on a
// docked robot, and 5 misses in 5 while it was driving back to its dock. So the
// probe is re-sent every Interval until the context ends.
type Discoverer struct {
	// Addr is where the probe is sent. Defaults to the IPv4 broadcast address.
	Addr *net.UDPAddr
	// Interval between re-sent probes. Defaults to 1s.
	Interval time.Duration
}

func (d *Discoverer) addr() *net.UDPAddr {
	if d.Addr != nil {
		return d.Addr
	}
	return &net.UDPAddr{IP: net.IPv4bcast, Port: DiscoveryPort}
}

func (d *Discoverer) interval() time.Duration {
	if d.Interval > 0 {
		return d.Interval
	}
	return time.Second
}

// Scan collects every robot that answers before ctx ends, de-duplicated by BLID.
func (d *Discoverer) Scan(ctx context.Context) ([]Info, error) {
	var found []Info
	seen := map[string]bool{}
	err := d.run(ctx, func(info Info) bool {
		if !seen[info.BLID] {
			seen[info.BLID] = true
			found = append(found, info)
		}
		return false
	})
	return found, err
}

// Find returns the robot with the given BLID, or ErrNotFound once ctx ends.
func (d *Discoverer) Find(ctx context.Context, blid string) (Info, error) {
	var match Info
	err := d.run(ctx, func(info Info) bool {
		if info.BLID == blid {
			match = info
			return true
		}
		return false
	})
	if err != nil {
		return Info{}, err
	}
	if match.BLID == "" {
		return Info{}, ErrNotFound
	}
	return match, nil
}

// run sends probes and hands every reply to visit until visit returns true or ctx ends.
// Reaching the end of ctx is not an error: it is how a scan finishes.
func (d *Discoverer) run(ctx context.Context, visit func(Info) bool) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	defer conn.Close()

	buf := make([]byte, 2048)
	for {
		if _, err := conn.WriteToUDP(discoveryProbe, d.addr()); err != nil {
			return err
		}
		next := time.Now().Add(d.interval())
		for {
			deadline := next
			if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
				deadline = dl
			}
			_ = conn.SetReadDeadline(deadline)
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				var ne net.Error
				if !errors.As(err, &ne) || !ne.Timeout() {
					return err
				}
				break // time to re-send, or ctx is over
			}
			if info, ok := parseInfo(buf[:n]); ok && visit(info) {
				return nil
			}
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}
