package kvs

import (
	"devices/shelly/types"
	"encoding/json"
	"fmt"
)

func ListKeys(via types.Channel, device types.Device, match string) (*KeyItems, error) {
	out, err := device.CallE(via, "KVS", "List", &KeyValuesMatching{
		Match: match,
	})
	if err != nil {
		log.Error(err, "Unable to List keys")
		return nil, err
	}
	keys := out.(*KeyItems)
	return keys, nil
}

func GetMany(via types.Channel, device types.Device) (*KeyValueItems, error) {
	out, err := device.CallE(via, "KVS", "GetMany", nil)
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

func SetKeyValue(via types.Channel, device types.Device, key string, value string) (*Status, error) {
	out, err := device.CallE(via, "KVS", "Set", &KeyValue{
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

func Delete(via types.Channel, device types.Device, key string) (*Status, error) {
	out, err := device.CallE(via, "KVS", "Delete", &Key{
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
