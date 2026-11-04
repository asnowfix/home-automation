package kvs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

func ListKeys(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, match string) (*ListResponse, error) {
	out, err := device.CallE(ctx, via, string(List), &ListOrGetManyRequest{
		Match: match,
	})
	if err != nil {
		log.Error(err, "Unable to List keys")
		return nil, err
	}
	keys, ok := out.(*ListResponse)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", reflect.TypeOf(&ListResponse{}), out)
	}
	return keys, nil
}

func GetManyValues(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, match string) (*GetManyResponse, error) {
	out, err := device.CallE(ctx, via, string(GetMany), &ListOrGetManyRequest{
		Match: match,
	})
	if err != nil {
		log.Error(err, "Unable to get many key-values")
		return nil, err
	}
	kvs, ok := out.(*GetManyResponse)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", reflect.TypeOf(&GetManyResponse{}), out)
	}

	return kvs, nil
}

// ErrNotFound is wrapped by the error GetValue returns when the device itself
// answered that the key does not exist. Test with errors.Is.
//
// Any other GetValue error — a timeout, a cancelled context, a transport
// failure, a device error that is not "not found" — means the lookup failed
// and says nothing about whether the key exists. Callers that turn a missing
// key into an action must not treat those as ErrNotFound (#663).
var ErrNotFound = errors.New("kvs: key not found")

// GetValue reads one KVS entry, through a per-device cache.
//
// On failure it returns an error wrapping ErrNotFound if (and only if) the
// device replied that the key does not exist; see ErrNotFound.
func GetValue(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, key string) (*GetResponse, error) {
	if entry, ok := cacheGet(device.Id(), key); ok {
		if entry.notFound {
			log.V(1).Info("KVS cache hit (not found)", "device", device.Id(), "key", key)
			if entry.definitive {
				return nil, fmt.Errorf("%w: key %q on device %q (cached)", ErrNotFound, key, device.Id())
			}
			return nil, fmt.Errorf("kvs: earlier lookup of key %q on device %q failed (cached)", key, device.Id())
		}
		log.V(1).Info("KVS cache hit", "device", device.Id(), "key", key)
		res := entry.value
		return &res, nil
	}

	out, err := device.CallE(ctx, via, string(Get), &GetRequest{
		Key: key,
	})
	if err != nil {
		var rpcErr *types.RpcError
		if errors.As(err, &rpcErr) && rpcErr.IsNotFound() {
			log.Info("KVS key not found on device (continuing)", "key", key)
			cachePutNotFound(device.Id(), key, true)
			return nil, fmt.Errorf("%w: key %q on device %q: %w", ErrNotFound, key, device.Id(), err)
		}
		log.Info("Unable to get on key (continuing)", "key", key)
		cachePutNotFound(device.Id(), key, false)
		return nil, err
	}
	res, ok := out.(*GetResponse)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", reflect.TypeOf(&GetResponse{}), out)
	}

	cachePut(device.Id(), key, *res)

	return res, nil
}

func SetKeyValue(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, key string, value string) (*Status, error) {
	out, err := device.CallE(ctx, via, string(Set), &KeyValue{
		Key:   key,
		Value: value,
	})
	if err != nil {
		log.Error(err, "Unable to set", "key", key, "value", value)
		return nil, err
	}
	status, ok := out.(*Status)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", reflect.TypeOf(&Status{}), out)
	}

	cacheInvalidate(device.Id(), key)
	noteSelfWrite(device.Id())

	return status, nil
}

func DeleteKey(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, key string) (*Status, error) {
	out, err := device.CallE(ctx, via, string(Delete), map[string]any{
		"key": key,
	})
	if err != nil {
		log.Error(err, "Unable to delete", "key", key)
		return nil, err
	}
	status, ok := out.(*Status)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", reflect.TypeOf(&Status{}), out)
	}
	s, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	cacheInvalidate(device.Id(), key)
	noteSelfWrite(device.Id())

	return status, nil
}
