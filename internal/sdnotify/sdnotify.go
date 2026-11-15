// Package sdnotify speaks the small part of systemd's sd_notify protocol the
// daemon needs (READY, WATCHDOG, STOPPING), without a dependency: a state
// string is one datagram written to the unix socket named by $NOTIFY_SOCKET.
//
// Everything is a no-op when NOTIFY_SOCKET is unset (running from a shell,
// on macOS, on Windows), so callers never need to check the platform.
package sdnotify

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/go-logr/logr"
)

// Notify sends one state string (e.g. "READY=1"). It returns false and a nil
// error when no notify socket is configured.
func Notify(state string) (bool, error) {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return false, nil
	}
	// An abstract socket is spelled "@name" in the environment, "\0name" on the wire.
	if addr[0] == '@' {
		addr = "\x00" + addr[1:]
	}
	conn, err := net.Dial("unixgram", addr)
	if err != nil {
		return false, fmt.Errorf("sd_notify dial: %w", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(state)); err != nil {
		return false, fmt.Errorf("sd_notify write: %w", err)
	}
	return true, nil
}

// Ready tells systemd that start-up is finished (Type=notify).
func Ready() error { _, err := Notify("READY=1"); return err }

// Stopping tells systemd that shutdown has begun.
func Stopping() error { _, err := Notify("STOPPING=1"); return err }

// Watchdog pets the systemd watchdog.
func Watchdog() error { _, err := Notify("WATCHDOG=1"); return err }

// WatchdogInterval returns how often to pet the watchdog: half of the
// WatchdogSec systemd advertises in $WATCHDOG_USEC, as sd_watchdog_enabled(3)
// recommends. Zero means the watchdog is not enabled for this process.
func WatchdogInterval() time.Duration {
	usec, err := strconv.ParseInt(os.Getenv("WATCHDOG_USEC"), 10, 64)
	if err != nil || usec <= 0 {
		return 0
	}
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		return 0
	}
	return time.Duration(usec) * time.Microsecond / 2
}

// Check reports whether one part of the daemon is alive. It must honour ctx.
type Check func(ctx context.Context) error

// Heartbeat pets the watchdog every interval for as long as every check
// passes, and stays silent otherwise, so systemd restarts a daemon that is
// running but not working. interval <= 0 disables it. Each beat gives its
// checks half the interval to answer.
//
// Checks are re-read on every beat through the checks func, so the alert
// evaluator (#712) can register its own later.
func Heartbeat(ctx context.Context, log logr.Logger, interval time.Duration, checks func() []Check) {
	if interval <= 0 {
		return
	}
	beat := func() {
		cctx, cancel := context.WithTimeout(ctx, interval/2)
		defer cancel()
		for i, check := range checks() {
			if err := check(cctx); err != nil {
				log.Error(err, "Health check failed, withholding watchdog heartbeat", "check", i)
				return
			}
		}
		if err := Watchdog(); err != nil {
			log.Error(err, "Watchdog heartbeat failed")
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	beat()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			beat()
		}
	}
}
