package types

import (
	"fmt"
	"strings"
)

// RpcError is the error a device returns in the "error" member of an RPC
// reply: the device was reached and answered, but refused or failed the call.
//
// It exists so callers can tell "the device said no" apart from "no answer"
// (timeout, cancelled context, connection reset), which are plain errors. The
// distinction matters whenever a failure is about to be turned into an action:
// a KVS key the device reports as missing is a fact, while a KVS read that
// timed out says nothing about the key (#663).
type RpcError struct {
	Code    int    // the device's error code
	Message string // the device's error message
	Reply   string // the raw reply payload, for diagnostics
}

var _ error = (*RpcError)(nil)

// Error keeps the exact wording the MQTT channel produced before RpcError
// existed, so anything matching on the text keeps working.
func (e *RpcError) Error() string {
	return fmt.Sprintf("device replied error '%v' (code:%v) to request '%v'", e.Message, e.Code, e.Reply)
}

// IsNotFound reports whether the device answered that the thing asked for does
// not exist (e.g. a KVS key that was never set).
//
// It matches on the message rather than on a specific code: Shelly reuses one
// "not found" wording across components, and no code for it is documented in
// this repository. A reply that says something else — busy, bad argument, an
// unknown code — is deliberately NOT a not-found, so the caller treats it as
// "could not tell" rather than as a fact about the key.
func (e *RpcError) IsNotFound() bool {
	return strings.Contains(strings.ToLower(e.Message), "not found")
}
