package groups

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"pkg/devices"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"reflect"
)

const KvsGroupPrefix = "group/"

func AddDevice(ctx context.Context, group string, device types.Device) error {
	_, err := myhome.TheClient.CallE(ctx, myhome.GroupAddDevice, &myhome.GroupDevice{
		Group:        group,
		Manufacturer: device.Manufacturer(),
		Id:           device.Id(),
	})
	return err
}

func RemoveDevice(ctx context.Context, group string, device types.Device) error {
	_, err := myhome.TheClient.CallE(ctx, myhome.GroupRemoveDevice, &myhome.GroupDevice{
		Group:        group,
		Manufacturer: device.Manufacturer(),
		Id:           device.Id(),
	})
	return err
}

func GetDeviceGroups(ctx context.Context, device devices.Device) ([]string, error) {
	sd, ok := device.(types.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	kvs, err := kvs.GetManyValues(ctx, hlog.Logger, types.ChannelDefault, sd, KvsGroupPrefix+"*")
	if err != nil {
		return nil, err
	}
	groups := make([]string, 0)
	for _, kv := range kvs.Items {
		group := kv.Key[len(KvsGroupPrefix):]
		groups = append(groups, group)
	}
	return groups, nil
}
