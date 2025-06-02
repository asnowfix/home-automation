package groups

import (
	"context"
	"hlog"
	"myhome"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
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

func GetDeviceGroups(ctx context.Context, device types.Device) ([]string, error) {
	kvs, err := kvs.GetManyValues(ctx, hlog.Logger, types.ChannelDefault, device, KvsGroupPrefix+"*")
	if err != nil {
		return nil, err
	}
	groups := make([]string, 0)
	for key, _ := range kvs.Items {
		group := key[len(KvsGroupPrefix):]
		groups = append(groups, group)
	}
	return groups, nil
}
