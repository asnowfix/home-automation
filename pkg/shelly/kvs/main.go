package kvs

import (
	"context"
	"encoding/json"
	"fmt"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

func ListKeys(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, match string) (*KeyItems, error) {
	out, err := device.CallE(ctx, via, string(List), &KeyValuesMatching{
		Match: match,
	})
	if err != nil {
		log.Error(err, "Unable to List keys")
		return nil, err
	}
	keys := out.(*KeyItems)
	return keys, nil
}

func GetManyValues(ctx context.Context, log logr.Logger, via types.Channel, device types.Device) (*KeyValueItems, error) {
	out, err := device.CallE(ctx, via, string(GetMany), nil)
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

func SetKeyValue(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, key string, value string) (*Status, error) {
	out, err := device.CallE(ctx, via, string(Set), &KeyValue{
		Key:   Key{Key: key},
		Value: Value{Value: value},
	})
	if err != nil {
		log.Error(err, "Unable to set", "key", key, "value", value)
		return nil, err
	}
	status := out.(*Status)
	return status, nil
}

func DeleteKey(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, key string) (*Status, error) {
	out, err := device.CallE(ctx, via, string(Delete), &Key{
		Key: key,
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
