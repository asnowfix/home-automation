package wifi

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

func TestDoGetConfig(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		d := types.NewFakeDevice()
		want := &Config{}
		d.SetResult(string(GetConfig), want)

		got, err := DoGetConfig(context.Background(), types.ChannelDefault, d)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
	})

	t.Run("device error propagates", func(t *testing.T) {
		d := types.NewFakeDevice()
		wantErr := errors.New("wifi config unavailable")
		d.SetError(string(GetConfig), wantErr)

		_, err := DoGetConfig(context.Background(), types.ChannelDefault, d)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})

	t.Run("unexpected result type", func(t *testing.T) {
		d := types.NewFakeDevice()
		d.SetResult(string(GetConfig), "not-a-config")

		_, err := DoGetConfig(context.Background(), types.ChannelDefault, d)
		if err == nil {
			t.Fatal("expected an error for unexpected result type")
		}
	})
}

func TestDoSetConfig(t *testing.T) {
	d := types.NewFakeDevice()
	want := &SetConfigResponse{}
	d.SetResult(string(SetConfig), want)

	got, err := DoSetConfig(context.Background(), types.ChannelDefault, d, &Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
	if _, ok := d.Calls[0].Params.(*SetConfigRequest); !ok {
		t.Errorf("expected *SetConfigRequest, got %T", d.Calls[0].Params)
	}
}

func TestDoGetStatus(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		d := types.NewFakeDevice()
		want := &Status{SSID: "home-net"}
		d.SetResult(GetStatus.String(), want)

		got, err := DoGetStatus(context.Background(), types.ChannelDefault, d)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
	})

	t.Run("unexpected result type", func(t *testing.T) {
		d := types.NewFakeDevice()
		d.SetResult(GetStatus.String(), "not-a-status")

		_, err := DoGetStatus(context.Background(), types.ChannelDefault, d)
		if err == nil {
			t.Fatal("expected an error for unexpected result type")
		}
	})
}
