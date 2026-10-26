package mcp

import (
	"context"
	"testing"

	shellymqtt "github.com/asnowfix/home-automation/pkg/shelly/mqtt"
)

// TestCarryMqttClient_CopiesClientFromRootCtx is the regression test for
// #565's review finding: myhome ctl mcp's server.ServeStdio(srv) builds its
// own root context internally (context.WithCancel(context.Background()),
// tied to SIGTERM/SIGINT) rather than deriving one from cmd.Context() —
// so the MQTT client ctl.Cmd's PersistentPreRunE injects into cmd.Context()
// never used to reach handleList/handleCall, and pkg/shelly/device.go's
// mqtt.GetClient(ctx) returned nil there (dereferenced unconditionally a
// line later — a panic that killed the MCP server subprocess on the first
// shelly_call/shelly_list against a Gen2 device in a fresh session).
//
// carryMqttClient is the server.StdioContextFunc that closes that gap. This
// exercises it directly, without starting a real stdio session: doing that
// would mean feeding JSON-RPC frames over an in-process pipe standing in for
// stdin/stdout and asserting on a live device's RPC response, which needs a
// live device (out of scope here, per this repo's no-hardware-in-CI rule) —
// so this is the boundary of what's testable without one.
func TestCarryMqttClient_CopiesClientFromRootCtx(t *testing.T) {
	mc := shellymqtt.NewMockClient()
	rootCtx := shellymqtt.NewContextWithClient(context.Background(), mc)

	fn := carryMqttClient(rootCtx)
	sessionCtx := fn(context.Background())

	got := shellymqtt.GetClient(sessionCtx)
	if got != mc {
		t.Fatalf("carryMqttClient did not carry the client over: got %v, want %v", got, mc)
	}
}

// TestCarryMqttClient_RootCtxWithoutClient_IsNoop covers the composition
// root's own PersistentPreRunE never having run (or having run without a
// client) — carryMqttClient must not panic and must return ctx unchanged,
// leaving pkg/shelly/device.go's new nil guard (not a panic) as the failure
// mode for whatever calls mqtt.GetClient(ctx) downstream.
func TestCarryMqttClient_RootCtxWithoutClient_IsNoop(t *testing.T) {
	fn := carryMqttClient(context.Background())
	sessionCtx := fn(context.Background())

	if got := shellymqtt.GetClient(sessionCtx); got != nil {
		t.Fatalf("expected no client on ctx, got %v", got)
	}
}

// TestCarryMqttClient_PreservesSessionCtxCancellation asserts
// carryMqttClient carries the client value across without replacing the
// stdio server's own per-session ctx outright — losing that would mean
// losing ServeStdio's SIGTERM/SIGINT-driven shutdown.
func TestCarryMqttClient_PreservesSessionCtxCancellation(t *testing.T) {
	mc := shellymqtt.NewMockClient()
	rootCtx := shellymqtt.NewContextWithClient(context.Background(), mc)

	fn := carryMqttClient(rootCtx)

	sessionBase, cancel := context.WithCancel(context.Background())
	sessionCtx := fn(sessionBase)
	cancel()

	select {
	case <-sessionCtx.Done():
	default:
		t.Fatal("expected the returned ctx to still observe the session ctx's own cancellation")
	}
}
