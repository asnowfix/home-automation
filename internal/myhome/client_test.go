package myhome

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
)

// TestLookupDevices_BareName_NoDaemon_NamesCauseAndRemedy is the regression test
// for issue #599: a bare device name (no ".local" suffix, not an IP) can only be
// resolved via the device.lookup/device.match RPC, which needs a daemon answering
// on the configured instance. When no daemon answers, the resulting error must
// name both the cause (no daemon answered) and the remedy (.local name or IP,
// which resolve without a daemon) -- not just the bare "timeout ... after Ns"
// message, which names neither.
//
// It is robust to the timeout duration and the request id, both of which vary
// run to run: it asserts on the remedy wording, not on those values.
func TestLookupDevices_BareName_NoDaemon_NamesCauseAndRemedy(t *testing.T) {
	ctx := logr.NewContext(context.Background(), logr.Discard())

	// Nothing ever answers on ServerTopic(), so CallE always times out.
	mc := mqtt.NewRecordingMockClient()

	// Keep the test fast: the timeout duration itself is not what's under test.
	c, err := NewClientE(ctx, logr.Discard(), mc, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewClientE: %v", err)
	}

	// LookupDevices calls TheClient.CallE (the package-level singleton), not
	// hc.CallE -- see internal/myhome/client.go. Point it at our test client
	// and restore whatever was there before, per the RPC-handler-test
	// convention (shared package-level state, no t.Parallel()).
	prev := TheClient
	TheClient = c
	t.Cleanup(func() { TheClient = prev })

	const bareName = "filtration-hiver"
	_, err = c.LookupDevices(ctx, bareName)
	if err == nil {
		t.Fatalf("LookupDevices(%q) with no daemon running: got nil error, want a timeout error", bareName)
	}

	msg := err.Error()

	// Cause: no daemon answered.
	if !strings.Contains(msg, "no daemon answered") {
		t.Errorf("error does not name the cause (%q not found in %q)", "no daemon answered", msg)
	}

	// Remedy: .local name or IP resolve without a daemon, with a concrete example.
	if !strings.Contains(msg, ".local") {
		t.Errorf("error does not mention the .local remedy in %q", msg)
	}
	if !strings.Contains(msg, "IP") {
		t.Errorf("error does not mention the IP remedy in %q", msg)
	}
	if !strings.Contains(msg, bareName+".local") {
		t.Errorf("error does not give a concrete .local example for %q in %q", bareName, msg)
	}

	// The underlying timeout detail must still be present -- it stays useful,
	// it's just not the whole story any more.
	if !strings.Contains(msg, "timeout waiting for response") {
		t.Errorf("error dropped the underlying timeout detail: %q", msg)
	}
}
