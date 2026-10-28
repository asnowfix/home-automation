package myhome

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/asnowfix/home-automation/pkg/devices"
)

// clientContextKey is the unexported type used to store a Client in a
// context.Context via NewContextWithClient/ClientFromContext.
type clientContextKey struct{}

// NewContextWithClient returns a copy of ctx carrying c as the RPC client to
// use for this command invocation. It replaces the former package-level
// TheClient global.
//
// Cobra's command tree gives every subcommand's RunE the same signature
// (func(cmd *cobra.Command, args []string) error), with no room for an
// extra constructor-injected parameter — each *cobra.Command is a
// package-level var built once at init() time. myhome/ctl/ctl.go (the CLI's
// composition root) is the single place that constructs the RPC client; it
// stores it here, mirroring how it already threads the logr.Logger via
// logr.NewContext, and every command reads it back via ClientFromContext at
// the top of its RunE. Everything below that boundary (Foreach,
// ConfigureDevice, blu.ResolveMac, ...) takes the Client as a plain,
// explicit function parameter instead of reaching back into ctx.
func NewContextWithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientContextKey{}, c)
}

// ClientFromContext returns the Client stored by NewContextWithClient. It
// returns an error rather than panicking so a CLI command can report a
// clear "myhome client not initialized" message instead of a nil-pointer
// crash if ctl.go's PersistentPreRunE was somehow skipped.
func ClientFromContext(ctx context.Context) (Client, error) {
	c, ok := ctx.Value(clientContextKey{}).(Client)
	if !ok || c == nil {
		return nil, fmt.Errorf("myhome client not found in context (was NewContextWithClient called?)")
	}
	return c, nil
}

// InstanceName is the myhome server instance name used for RPC topics.
// Default is "myhome". Can be changed to target a different server instance.
var InstanceName string = MYHOME

type Client interface {
	LookupDevices(ctx context.Context, name string) (*[]devices.Device, error)
	ForgetDevices(ctx context.Context, name string) error
	CallE(ctx context.Context, method Verb, params any) (any, error)
}

// Call is a typed convenience wrapper around Client.CallE. P is the request
// parameter type, R the expected result type; both are normally inferred
// from the arguments, so a call site reads:
//
//	rooms, err := myhome.Call[any, *myhome.RoomListResult](ctx, client, myhome.RoomList, nil)
//
// CallE itself stays untyped (it must satisfy the Client interface for every
// verb), so Call is what gives callers their compile-time result type back.
// For the wire client, CallE's result is the raw json.RawMessage taken
// directly from the response envelope, so Call does exactly one
// json.Unmarshal of it into R — no marshal-then-unmarshal round trip. Test
// doubles (FakeClient) and the in-process DeviceManager short-circuit return
// an already-typed R value instead; Call passes that through unchanged.
func Call[P, R any](ctx context.Context, c Client, verb Verb, p P) (R, error) {
	var zero R
	out, err := c.CallE(ctx, verb, p)
	if err != nil {
		return zero, err
	}
	switch v := out.(type) {
	case nil:
		return zero, nil
	case R:
		return v, nil
	case json.RawMessage:
		if len(v) == 0 {
			return zero, nil
		}
		var r R
		if err := json.Unmarshal(v, &r); err != nil {
			return zero, fmt.Errorf("unmarshal result for %s: %w", verb, err)
		}
		return r, nil
	default:
		// Defensive fallback: a handler or test double returned a value whose
		// static type isn't R but is still JSON-compatible with it.
		raw, err := json.Marshal(v)
		if err != nil {
			return zero, fmt.Errorf("re-marshal result for %s: %w", verb, err)
		}
		var r R
		if err := json.Unmarshal(raw, &r); err != nil {
			return zero, fmt.Errorf("unmarshal result for %s: %w", verb, err)
		}
		return r, nil
	}
}

func ServerTopic() string {
	return fmt.Sprintf("%s/rpc", InstanceName)
}

func ClientTopic(clientId string) string {
	return fmt.Sprintf("%s/rpc", clientId)
}

type Dialog struct {
	Id  string `json:"id"`
	Src string `json:"src"`
	Dst string `json:"dst"`
}

// request is the wire envelope for an RPC call. Params is carried as raw
// JSON so the server decodes it exactly once, directly into the handler's
// declared parameter type (see Register) instead of unmarshalling the whole
// message a second time to re-decode it with proper typing.
type request struct {
	Dialog
	Method Verb            `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// response is the wire envelope for an RPC reply. Result is raw JSON for the
// same reason request.Params is: the client hands it directly to Call, which
// unmarshals it once into the caller's chosen result type.
type response struct {
	Dialog
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

func ValidateDialog(d Dialog) error {
	if d.Id == "" {
		return fmt.Errorf("invalid dialog: id=%v", d.Id)
	}
	if d.Src == "" {
		return fmt.Errorf("invalid dialog: src=%v", d.Src)
	}
	if d.Dst == "" {
		return fmt.Errorf("invalid dialog: dst=%v", d.Dst)
	}
	return nil
}

func (d Dialog) String() string {
	return fmt.Sprintf("{id=%s, src=%s, dst=%s}", d.Id, d.Src, d.Dst)
}
