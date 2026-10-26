//go:build live

package roomba

// Live test against a real robot on the LAN. Not part of `make test`.
//
//	ROOMBA_CREDS=~/.homepi/roomba-creds.json go test -tags live -run TestLive -v ./pkg/roomba
//
// The credentials file is JSON {"blid":"…","password":"…"} and is never printed.
// The test only reads state and sends `dock`, which has no effect on a docked
// robot and sends an undocked one home.

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
)

func TestLive(t *testing.T) {
	path := os.Getenv("ROOMBA_CREDS")
	if path == "" {
		t.Skip("ROOMBA_CREDS not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var creds struct{ BLID, Password string }
	if err := json.Unmarshal(raw, &creds); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	host := os.Getenv("ROOMBA_HOST")
	if host == "" {
		dctx, dcancel := context.WithTimeout(ctx, 8*time.Second)
		info, err := (&Discoverer{}).Find(dctx, creds.BLID)
		dcancel()
		if err != nil {
			t.Fatalf("discovery: %v (set ROOMBA_HOST to bypass)", err)
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
