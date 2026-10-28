//go:build live

package roomba

// Live test against a real robot on the LAN. Not part of `make test`.
//
//	MYHOME_VACUUM_BLID=… MYHOME_VACUUM_PASSWORD=… go test -tags live -run TestLive -v ./pkg/roomba
//
// It reads the same variables as the daemon (#626): MYHOME_VACUUM_BLID and
// MYHOME_VACUUM_PASSWORD, plus the optional MYHOME_VACUUM_HOST to skip
// discovery. The simplest source is the daemon's own .env, e.g.
// `set -a; . /var/lib/myhome/.env; set +a` first. The password is never printed.
// The test only reads state and sends `dock`, which has no effect on a docked
// robot and sends an undocked one home.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
)

func TestLive(t *testing.T) {
	creds := struct{ BLID, Password string }{
		BLID:     os.Getenv("MYHOME_VACUUM_BLID"),
		Password: os.Getenv("MYHOME_VACUUM_PASSWORD"),
	}
	if creds.BLID == "" || creds.Password == "" {
		t.Skip("MYHOME_VACUUM_BLID and MYHOME_VACUUM_PASSWORD not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	host := os.Getenv("MYHOME_VACUUM_HOST")
	if host == "" {
		dctx, dcancel := context.WithTimeout(ctx, 8*time.Second)
		info, err := (&Discoverer{}).Find(dctx, creds.BLID)
		dcancel()
		if err != nil {
			t.Fatalf("discovery: %v (set MYHOME_VACUUM_HOST to bypass)", err)
		}
		t.Logf("discovered %s at %s (sku %s, fw %s)", info.Name, info.IP, info.SKU, info.Firmware)
		host = info.IP
	}

	c := NewClient(testr.New(t), creds.BLID, creds.Password, host, nil)
	start := time.Now()
	if err := c.Connect(ctx, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	t.Logf("connected in %s", time.Since(start).Round(time.Millisecond))

	for {
		if st, ok := c.Status(); ok {
			t.Logf("status: name=%s battery=%d phase=%s/%s error=%d bin_full=%v maps=%v last=%s",
				st.Name, st.Battery, st.Phase, st.Cycle, st.Error, st.BinFull, st.Maps, st.LastCommand.Command)
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("no state received")
		case <-time.After(200 * time.Millisecond):
		}
	}

	start = time.Now()
	if err := c.Command(ctx, CmdDock, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("dock acknowledged in %s", time.Since(start).Round(time.Millisecond))
}
