package kvs

import (
	"context"
	"encoding/json"
	"fmt"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

func ListKeys(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, match string) (*KeyItems, error) {
	out, err := device.CallE(ctx, via, string(List), map[string]any{
		"match": match,
	})
	if err != nil {
		log.Error(err, "Unable to List keys")
		return nil, err
	}
	keys := out.(*KeyItems)
	return keys, nil
}

func GetManyValues(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, match string) (*KeyValueItems, error) {
	out, err := device.CallE(ctx, via, string(GetMany), map[string]any{
		"match": match,
	})
	if err != nil {
		log.Error(err, "Unable to get many key-values")
		return nil, err
	}
	kvs := out.(*KeyValueItems)
	s, err := json.Marshal(kvs)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return kvs, nil
}

func GetValue(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, key string) (*Value, error) {
	out, err := device.CallE(ctx, via, string(Get), map[string]any{
		"key": key,
	})
	if err != nil {
		log.Error(err, "Unable to get on key")
		return nil, err
	}
	value := out.(*Value)
	s, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return value, nil
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
	status := out.(*Status)
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
	status := out.(*Status)
	s, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return status, nil
}
