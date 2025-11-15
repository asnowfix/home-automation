package kvs

import (
	"context"
	"encoding/json"
	"fmt"
	"pkg/shelly/types"
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

func GetValue(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, key string) (*GetResponse, error) {
	out, err := device.CallE(ctx, via, string(Get), &GetRequest{
		Key: key,
	})
	if err != nil {
		log.Info("Unable to get on key (continuing)", "key", key)
		return nil, err
	}
	res, ok := out.(*GetResponse)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", reflect.TypeOf(&GetResponse{}), out)
	}
	// s, err := json.Marshal(res)
	// if err != nil {
	// 	return nil, err
	// }
	// fmt.Print(string(s))

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

	return status, nil
}
