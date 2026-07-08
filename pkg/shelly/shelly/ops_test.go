package shelly

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

func TestDoGetComponents(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		d := types.NewFakeDevice()
		want := &ComponentsResponse{Total: 1}
		d.SetResult(GetComponents.String(), want)

		got, err := DoGetComponents(context.Background(), d, &ComponentsRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
	})

	t.Run("zero total is an error", func(t *testing.T) {
		d := types.NewFakeDevice()
		d.SetResult(GetComponents.String(), &ComponentsResponse{Total: 0})

		_, err := DoGetComponents(context.Background(), d, &ComponentsRequest{})
		if err == nil {
			t.Fatal("expected an error when Total is 0")
		}
	})

	t.Run("nil reply is an error", func(t *testing.T) {
		d := types.NewFakeDevice()
		d.SetResult(GetComponents.String(), nil)

		_, err := DoGetComponents(context.Background(), d, &ComponentsRequest{})
		if err == nil {
			t.Fatal("expected an error for a nil reply")
		}
	})

	t.Run("device error propagates", func(t *testing.T) {
		d := types.NewFakeDevice()
		wantErr := errors.New("components unavailable")
		d.SetError(GetComponents.String(), wantErr)

		_, err := DoGetComponents(context.Background(), d, &ComponentsRequest{})
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})
}

func TestDoCheckForUpdate(t *testing.T) {
	d := types.NewFakeDevice()
	want := &CheckForUpdateResponse{}
	d.SetResult(CheckForUpdate.String(), want)

	got, err := DoCheckForUpdate(context.Background(), types.ChannelDefault, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

func TestDoUpdate(t *testing.T) {
	d := types.NewFakeDevice()
	d.SetResult(Update.String(), nil)

	err := DoUpdate(context.Background(), types.ChannelDefault, d, "beta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req, ok := d.Calls[0].Params.(*UpdateRequest)
	if !ok || req.Stage != "beta" {
		t.Errorf("expected UpdateRequest{Stage: beta}, got %+v", d.Calls[0].Params)
	}
}

func TestDoReboot(t *testing.T) {
	d := types.NewFakeDevice()
	d.SetResult(Reboot.String(), nil)

	if err := DoReboot(context.Background(), d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Calls[0].Method != Reboot.String() {
		t.Errorf("expected Shelly.Reboot call, got %+v", d.Calls[0])
	}
}

func TestGetDeviceInfo(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		d := types.NewFakeDevice()
		want := &DeviceInfo{Id: "shellyplus1-abc", Product: Product{MacAddress: "AA:BB:CC:DD:EE:FF"}}
		d.SetResult(getDeviceInfo.String(), want)

		got, err := GetDeviceInfo(context.Background(), d, types.ChannelDefault)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
	})

	t.Run("missing id and mac is an error", func(t *testing.T) {
		d := types.NewFakeDevice()
		d.SetResult(getDeviceInfo.String(), &DeviceInfo{})

		_, err := GetDeviceInfo(context.Background(), d, types.ChannelDefault)
		if err == nil {
			t.Fatal("expected an error for an empty device info")
		}
	})
}

func TestGetSwitchesSummary(t *testing.T) {
	d := types.NewFakeDevice()
	d.SetResult(GetComponents.String(), &ComponentsResponse{
		Total: 1,
		Config: Config{
			Switch0: &sswitch.Config{Id: 0, Name: "Pump"},
		},
		Status: Status{
			Switch0: &sswitch.Status{Id: 0, Output: true},
		},
	})

	ctx := logr.NewContext(context.Background(), logr.Discard())
	got, err := GetSwitchesSummary(ctx, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sw, ok := got[0]
	if !ok {
		t.Fatalf("expected switch 0 in summary, got %+v", got)
	}
	if sw.Name != "Pump" || !sw.On {
		t.Errorf("expected Pump/on, got %+v", sw)
	}
}

func TestGetSwitchesSummary_UnnamedSwitchDefaultsName(t *testing.T) {
	d := types.NewFakeDevice()
	d.SetResult(GetComponents.String(), &ComponentsResponse{
		Total: 1,
		Config: Config{
			Switch1: &sswitch.Config{Id: 1},
		},
	})

	ctx := logr.NewContext(context.Background(), logr.Discard())
	got, err := GetSwitchesSummary(ctx, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sw, ok := got[1]
	if !ok {
		t.Fatalf("expected switch 1 in summary, got %+v", got)
	}
	if sw.Name != "switch:1" {
		t.Errorf("expected default name 'switch:1', got %q", sw.Name)
	}
}
