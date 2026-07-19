package ethernet

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/asnowfix/home-automation/pkg/shelly/typestest"
)

func TestGetConfig(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		want := &Config{Enable: true, Ip: "192.168.1.20"}
		d.SetResult(getConfig.String(), want)

		got, err := GetConfig(context.Background(), types.ChannelDefault, d)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
	})

	t.Run("device error propagates", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		wantErr := errors.New("eth config unavailable")
		d.SetError(getConfig.String(), wantErr)

		_, err := GetConfig(context.Background(), types.ChannelDefault, d)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})
}

func TestSetConfig(t *testing.T) {
	d := typestest.NewFakeDevice()
	d.SetResult(setConfig.String(), &SetConfigResponse{Success: true})

	err := SetConfig(context.Background(), d, types.ChannelDefault, &Config{Enable: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := d.Calls[0].Params.(*Config); !ok {
		t.Errorf("expected the *Config to be forwarded as-is, got %T", d.Calls[0].Params)
	}
}

func TestSetConfig_DeviceError(t *testing.T) {
	d := typestest.NewFakeDevice()
	wantErr := errors.New("eth set config failed")
	d.SetError(setConfig.String(), wantErr)

	err := SetConfig(context.Background(), d, types.ChannelDefault, &Config{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestGetStatus(t *testing.T) {
	d := typestest.NewFakeDevice()
	want := &Status{IP: "192.168.1.20"}
	d.SetResult(getStatus.String(), want)

	got, err := GetStatus(context.Background(), d, types.ChannelDefault)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

func TestDoGetStatus(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		want := &Status{IP: "192.168.1.20"}
		d.SetResult(getStatus.String(), want)

		got, err := DoGetStatus(context.Background(), types.ChannelDefault, d)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
	})

	t.Run("device error propagates", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		wantErr := errors.New("eth status unavailable")
		d.SetError(getStatus.String(), wantErr)

		_, err := DoGetStatus(context.Background(), types.ChannelDefault, d)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})
}
