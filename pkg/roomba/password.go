package roomba

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// ErrNeedsButton means the robot refused to hand out its password because its
// HOME button was not held (~2 s, until it plays a tone) shortly before the request.
var ErrNeedsButton = errors.New("roomba: hold the robot's HOME button ~2s until it plays a tone, then retry within 2 minutes")

// passwordRequest is the magic packet that asks for the local password.
var passwordRequest, _ = hex.DecodeString("f005efcc3b2900")

// minPasswordLen is the shortest printable run accepted as a password. The i7's is 30.
const minPasswordLen = 20

// ParsePassword extracts the password from the robot's full reply to the magic
// packet: the longest run of printable ASCII, at least minPasswordLen long.
//
// Do not slice at a fixed offset, as dorita980 v4 does (13 or 9 bytes): on a
// Roomba i7 that produced a wrong password, and MQTT login failed with
// "Bad username or password". A reply of 7 bytes or fewer is the robot's
// "not now" answer, returned when the HOME button was not pressed.
func ParsePassword(reply []byte) (string, error) {
	if len(reply) <= len(passwordRequest) {
		return "", ErrNeedsButton
	}
	best, start := "", -1
	for i := 0; i <= len(reply); i++ {
		printable := i < len(reply) && reply[i] >= 0x20 && reply[i] <= 0x7e
		switch {
		case printable && start < 0:
			start = i
		case !printable && start >= 0:
			if i-start > len(best) {
				best = string(reply[start:i])
			}
			start = -1
		}
	}
	if len(best) < minPasswordLen {
		return "", fmt.Errorf("roomba: no password in a %d-byte reply", len(reply))
	}
	return best, nil
}

// GetPassword runs the one-time password handshake against host. The robot must
// have been armed with its HOME button just before; otherwise ErrNeedsButton.
// blid, when known, is checked against the robot's certificate (see TLSConfig).
//
// The robot sends the reply and then keeps the connection open, so the reply is
// read until the connection closes or ctx's deadline (default 9s) passes.
func GetPassword(ctx context.Context, host, blid string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 9*time.Second)
		defer cancel()
	}
	d := tls.Dialer{Config: TLSConfig(blid)}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(MQTTPort)))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := conn.Write(passwordRequest); err != nil {
		return "", err
	}
	reply, err := io.ReadAll(conn)
	var ne net.Error
	if err != nil && !(errors.As(err, &ne) && ne.Timeout()) {
		return "", err
	}
	return ParsePassword(reply)
}
