package system

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
		want := &Config{ConfigRevision: 7}
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
		wantErr := errors.New("sys config unavailable")
		d.SetError(getConfig.String(), wantErr)

		_, err := GetConfig(context.Background(), types.ChannelDefault, d)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})

	t.Run("RpcUdp with empty destination address is normalized to nil", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		cfg := &Config{
			RpcUdp: &struct {
				DestinationAddress string `json:"dst_addr"`
				ListenPort         uint16 `json:"listen_port,omitempty"`
			}{DestinationAddress: ""},
		}
		d.SetResult(getConfig.String(), cfg)

		got, err := GetConfig(context.Background(), types.ChannelDefault, d)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.RpcUdp != nil {
			t.Errorf("expected RpcUdp normalized to nil when dst_addr is empty, got %+v", got.RpcUdp)
		}
	})

	t.Run("RpcUdp with a real destination address is preserved", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		cfg := &Config{
			RpcUdp: &struct {
				DestinationAddress string `json:"dst_addr"`
				ListenPort         uint16 `json:"listen_port,omitempty"`
			}{DestinationAddress: "192.168.1.10:1010"},
		}
		d.SetResult(getConfig.String(), cfg)

		got, err := GetConfig(context.Background(), types.ChannelDefault, d)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.RpcUdp == nil || got.RpcUdp.DestinationAddress != "192.168.1.10:1010" {
			t.Errorf("expected RpcUdp preserved, got %+v", got.RpcUdp)
		}
	})
}

func TestSetConfig(t *testing.T) {
	d := typestest.NewFakeDevice()
	want := &SetConfigResponse{RestartRequired: true}
	d.SetResult(setConfig.String(), want)

	got, err := SetConfig(context.Background(), types.ChannelDefault, d, &Config{ConfigRevision: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
	req, ok := d.Calls[0].Params.(*SetConfigRequest)
	if !ok || req.Config.ConfigRevision != 3 {
		t.Errorf("expected SetConfigRequest wrapping the given config, got %+v", d.Calls[0].Params)
	}
}

func TestGetStatus(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		want := &Status{UpTime: 100}
		d.SetResult(getStatus.String(), want)

		got, err := GetStatus(context.Background(), types.ChannelDefault, d)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
	})

	t.Run("unexpected result type", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		d.SetResult(getStatus.String(), "not-a-status")

		_, err := GetStatus(context.Background(), types.ChannelDefault, d)
		if err == nil {
			t.Fatal("expected an error for unexpected result type")
		}
	})
}
