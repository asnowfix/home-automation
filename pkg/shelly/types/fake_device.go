package types

import (
	"context"
	"fmt"
	"net"
)

// FakeDeviceCall records a single CallE invocation made against a FakeDevice.
type FakeDeviceCall struct {
	Via    Channel
	Method string
	Params any
}

// FakeDevice is a test double for the Device interface. It records every
// CallE invocation and returns a canned result or error configured per
// method via SetResult/SetError, so pkg/shelly/* op-function tests can
// assert request encoding and response decoding without a real transport.
//
// Only CallE and the accessors backed by IdValue/NameValue/HostValue carry
// real behavior; the remaining Device methods are no-op stubs sufficient to
// satisfy the interface.
type FakeDevice struct {
	IdValue   string
	NameValue string
	HostValue string

	Calls   []FakeDeviceCall
	results map[string]any
	errs    map[string]error
}

// NewFakeDevice returns a FakeDevice ready for use.
func NewFakeDevice() *FakeDevice {
	return &FakeDevice{
		results: make(map[string]any),
		errs:    make(map[string]error),
	}
}

// SetResult configures CallE to return result for method (and clears any
// previously configured error for that method).
func (f *FakeDevice) SetResult(method string, result any) {
	f.results[method] = result
	delete(f.errs, method)
}

// SetError configures CallE to return err for method (and clears any
// previously configured result for that method).
func (f *FakeDevice) SetError(method string, err error) {
	f.errs[method] = err
	delete(f.results, method)
}

// CallE implements Device. It records the call and returns whatever was
// configured via SetResult/SetError; if nothing was configured for method it
// returns an error naming the unconfigured method.
func (f *FakeDevice) CallE(ctx context.Context, via Channel, method string, params any) (any, error) {
	f.Calls = append(f.Calls, FakeDeviceCall{Via: via, Method: method, Params: params})
	if err, ok := f.errs[method]; ok {
		return nil, err
	}
	if result, ok := f.results[method]; ok {
		return result, nil
	}
	return nil, fmt.Errorf("FakeDevice: no result configured for method %s", method)
}

func (f *FakeDevice) String() string        { return f.NameValue }
func (f *FakeDevice) Name() string          { return f.NameValue }
func (f *FakeDevice) Host() string          { return f.HostValue }
func (f *FakeDevice) Manufacturer() string  { return "fake" }
func (f *FakeDevice) Id() string            { return f.IdValue }
func (f *FakeDevice) Mac() net.HardwareAddr { return nil }
func (f *FakeDevice) ReplyTo() string       { return "" }
func (f *FakeDevice) To() chan<- []byte     { return nil }
func (f *FakeDevice) From() <-chan []byte   { return nil }

func (f *FakeDevice) StartDialog(ctx context.Context) uint32    { return 0 }
func (f *FakeDevice) StopDialog(ctx context.Context, id uint32) {}

func (f *FakeDevice) IsHttpReady() bool           { return true }
func (f *FakeDevice) IsMqttReady() bool           { return true }
func (f *FakeDevice) Channel(ctx context.Context, via Channel) Channel { return via }

func (f *FakeDevice) UpdateName(name string) { f.NameValue = name }
func (f *FakeDevice) UpdateHost(host string) { f.HostValue = host }
func (f *FakeDevice) ClearHost()             { f.HostValue = "" }
func (f *FakeDevice) UpdateMac(mac string)   {}
func (f *FakeDevice) UpdateId(id string)     { f.IdValue = id }

func (f *FakeDevice) IsModified() bool { return false }
func (f *FakeDevice) ResetModified()   {}

var _ Device = (*FakeDevice)(nil)
