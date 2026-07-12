package myhome

import (
	"context"
	"fmt"

	"github.com/asnowfix/home-automation/pkg/devices"
)

// FakeCall records a single CallE invocation made against a FakeClient.
type FakeCall struct {
	Method Verb
	Params any
}

// FakeClient is a test double for Client. It records every CallE invocation
// and returns a canned result or error configured per Verb via SetResult and
// SetError, so CLI command tests can assert which RPC verb and params a
// command issued without a real MQTT transport.
type FakeClient struct {
	Calls   []FakeCall
	results map[Verb]any
	errs    map[Verb]error
}

// NewFakeClient returns a FakeClient ready for use.
func NewFakeClient() *FakeClient {
	return &FakeClient{
		results: make(map[Verb]any),
		errs:    make(map[Verb]error),
	}
}

// SetResult configures CallE to return result for method (and clears any
// previously configured error for that method).
func (f *FakeClient) SetResult(method Verb, result any) {
	f.results[method] = result
	delete(f.errs, method)
}

// SetError configures CallE to return err for method (and clears any
// previously configured result for that method).
func (f *FakeClient) SetError(method Verb, err error) {
	f.errs[method] = err
	delete(f.results, method)
}

// CallE implements Client. It records the call and returns whatever was
// configured via SetResult/SetError; if nothing was configured for method it
// returns an error naming the unconfigured verb.
func (f *FakeClient) CallE(ctx context.Context, method Verb, params any) (any, error) {
	f.Calls = append(f.Calls, FakeCall{Method: method, Params: params})
	if err, ok := f.errs[method]; ok {
		return nil, err
	}
	if result, ok := f.results[method]; ok {
		return result, nil
	}
	return nil, fmt.Errorf("FakeClient: no result configured for method %s", method)
}

// LookupDevices implements Client. Not used by CLI command tests today;
// configure via a wrapper/embedding if a future test needs it.
func (f *FakeClient) LookupDevices(ctx context.Context, name string) (*[]devices.Device, error) {
	return nil, fmt.Errorf("FakeClient.LookupDevices not implemented")
}

// ForgetDevices implements Client. Not used by CLI command tests today;
// configure via a wrapper/embedding if a future test needs it.
func (f *FakeClient) ForgetDevices(ctx context.Context, name string) error {
	return fmt.Errorf("FakeClient.ForgetDevices not implemented")
}
