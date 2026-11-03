package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestRpcErrorMessageUnchanged(t *testing.T) {
	// The wording is what the MQTT channel produced before RpcError existed;
	// anything matching on the text (logs, strings.Contains callers) relies on it.
	e := &RpcError{Code: -105, Message: "Key not found", Reply: `{"id":1}`}
	want := `device replied error 'Key not found' (code:-105) to request '{"id":1}'`
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestRpcErrorIsNotFound(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"Key not found", true},
		{"key NOT FOUND", true},
		{"Argument 'id', value '7' not found!", true},
		{"Resource busy", false},
		{"Missing or bad argument 'code'!", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := (&RpcError{Message: tc.msg}).IsNotFound(); got != tc.want {
			t.Errorf("IsNotFound(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestRpcErrorSurvivesWrapping(t *testing.T) {
	var target *RpcError
	wrapped := fmt.Errorf("lookup: %w", &RpcError{Code: -1, Message: "x"})
	if !errors.As(wrapped, &target) || target.Code != -1 {
		t.Errorf("errors.As did not recover the RpcError from %v", wrapped)
	}
}
