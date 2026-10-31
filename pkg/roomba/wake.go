package roomba

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// Wake-probe bounds (#630). One probe is at most wakeCount echo requests
// wakeInterval apart, then waits for a reply until WakeBudget is spent.
const (
	wakeCount    = 3
	wakeInterval = 200 * time.Millisecond
	// WakeBudget is the longest a Wake call takes.
	WakeBudget = 2 * time.Second
)

// ErrWakeNotPermitted means the process may not open a raw ICMP socket (it
// needs root or CAP_NET_RAW). The probe is then skipped, not failed.
var ErrWakeNotPermitted = errors.New("roomba: ICMP wake probe needs root or CAP_NET_RAW")

// WakeResult is what one Wake call observed.
type WakeResult struct {
	Sent    int           // echo requests sent
	Replied bool          // an echo reply came back from the robot
	RTT     time.Duration // round trip of the first reply
}

// Wake sends a short burst of ICMP echo requests to host (an IPv4 address) and
// returns at the first reply, or when WakeBudget or ctx runs out.
//
// Why (#630, one observation, 2026-10-30): after a daemon restart a docked
// robot refused MQTT connections for ~2 minutes while answering pings in
// 570-860 ms (Wi-Fi power save); after a few pings the round trips dropped to
// 5-60 ms and the next connect succeeded. Unicast traffic may make the access
// point deliver buffered frames and wake the robot's radio. The connect
// attempts themselves (TCP SYNs answered by RSTs) did not wake it, which is
// why this sends ICMP rather than another TCP probe. Unverified: the effect
// may have been a coincidence. It is not Wake-on-LAN.
//
// It uses a raw socket (standard library only), so it needs root or
// CAP_NET_RAW; without them it returns ErrWakeNotPermitted at once.
func Wake(ctx context.Context, host string) (WakeResult, error) {
	var res WakeResult
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return res, fmt.Errorf("roomba: wake: %q is not an IPv4 address", host)
	}
	ctx, cancel := context.WithTimeout(ctx, WakeBudget)
	defer cancel()

	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return res, ErrWakeNotPermitted
		}
		return res, fmt.Errorf("roomba: wake: %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetReadDeadline(deadline)

	id := uint16(os.Getpid() & 0xffff)
	sentAt := map[uint16]time.Time{}
	replies := make(chan time.Duration, 1)
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return // deadline reached or socket closed
			}
			if a, ok := from.(*net.IPAddr); !ok || !a.IP.Equal(ip) {
				continue
			}
			rid, seq, ok := parseEchoReply(buf[:n])
			if !ok || rid != id {
				continue
			}
			select {
			case replies <- time.Duration(seq): // the sequence number; RTT computed below
			default:
			}
			return
		}
	}()

	tick := time.NewTicker(wakeInterval)
	defer tick.Stop()
	for seq := uint16(1); ; seq++ {
		if int(seq) <= wakeCount {
			sentAt[seq] = time.Now()
			if _, err := conn.WriteTo(echoRequest(id, seq), &net.IPAddr{IP: ip}); err != nil {
				return res, fmt.Errorf("roomba: wake: %w", err)
			}
			res.Sent++
		}
		select {
		case s := <-replies:
			res.Replied = true
			if t, ok := sentAt[uint16(s)]; ok {
				res.RTT = time.Since(t)
			}
			return res, nil
		case <-ctx.Done():
			return res, nil
		case <-tick.C:
		}
	}
}

// echoRequest builds an ICMPv4 echo request (type 8) with an 8-byte payload.
func echoRequest(id, seq uint16) []byte {
	b := make([]byte, 16)
	b[0] = 8 // echo request
	binary.BigEndian.PutUint16(b[4:], id)
	binary.BigEndian.PutUint16(b[6:], seq)
	copy(b[8:], "myhome!!")
	binary.BigEndian.PutUint16(b[2:], icmpChecksum(b))
	return b
}

// parseEchoReply reads an ICMPv4 echo reply (type 0). On Linux, reading an
// "ip4:icmp" socket returns the ICMP message without the IP header.
func parseEchoReply(b []byte) (id, seq uint16, ok bool) {
	if len(b) < 8 || b[0] != 0 || b[1] != 0 {
		return 0, 0, false
	}
	return binary.BigEndian.Uint16(b[4:]), binary.BigEndian.Uint16(b[6:]), true
}

func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
