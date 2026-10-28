package kvs

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/asnowfix/home-automation/pkg/shelly/typestest"
	"github.com/go-logr/logr"
)

func TestGetValue(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		want := &GetResponse{Value: "17"}
		d.SetResult(string(Get), want)

		got, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "script/heater/eco")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
		params, ok := d.Calls[0].Params.(*GetRequest)
		if !ok || params.Key != "script/heater/eco" {
			t.Errorf("expected GetRequest with key 'script/heater/eco', got %+v", d.Calls[0].Params)
		}
	})

	t.Run("device error propagates", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		wantErr := errors.New("kvs unreachable")
		d.SetError(string(Get), wantErr)

		_, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "key")
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})

	t.Run("unexpected result type", func(t *testing.T) {
		d := typestest.NewFakeDevice()
		d.SetResult(string(Get), "not-a-response")

		_, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "key")
		if err == nil {
			t.Fatal("expected an error for unexpected result type")
		}
	})
}

func TestSetKeyValue(t *testing.T) {
	d := typestest.NewFakeDevice()
	want := &Status{}
	d.SetResult(string(Set), want)

	got, err := SetKeyValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "key", "value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
	params, ok := d.Calls[0].Params.(*KeyValue)
	if !ok || params.Key != "key" || params.Value != "value" {
		t.Errorf("expected KeyValue{key,value}, got %+v", d.Calls[0].Params)
	}
}

func TestListKeys(t *testing.T) {
	d := typestest.NewFakeDevice()
	want := &ListResponse{Keys: map[string]Status{"a": {}}}
	d.SetResult(string(List), want)

	got, err := ListKeys(context.Background(), logr.Discard(), types.ChannelDefault, d, "script/*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
	params, ok := d.Calls[0].Params.(*ListOrGetManyRequest)
	if !ok || params.Match != "script/*" {
		t.Errorf("expected match 'script/*', got %+v", d.Calls[0].Params)
	}
}

func TestGetManyValues(t *testing.T) {
	d := typestest.NewFakeDevice()
	want := &GetManyResponse{Items: FlexibleMap{"a": "1"}}
	d.SetResult(string(GetMany), want)

	got, err := GetManyValues(context.Background(), logr.Discard(), types.ChannelDefault, d, "script/*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

func TestDeleteKey(t *testing.T) {
	d := typestest.NewFakeDevice()
	want := &Status{}
	d.SetResult(string(Delete), want)

	got, err := DeleteKey(context.Background(), logr.Discard(), types.ChannelDefault, d, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
	params, ok := d.Calls[0].Params.(map[string]any)
	if !ok || params["key"] != "key" {
		t.Errorf("expected map with key='key', got %+v", d.Calls[0].Params)
	}
}
