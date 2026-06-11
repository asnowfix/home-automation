package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

type MethodHandler func(ctx context.Context, in any) (any, error)

type MethodSignature struct {
	NewParams func() any
	NewResult func() any
}

type Method struct {
	Name      Verb
	Signature MethodSignature
	ActionE   MethodHandler
}

func Methods(name Verb) (*Method, error) {
	m, exists := methods[name]
	if !exists {
		return nil, fmt.Errorf("unknown or unregistered method %s", name)
	}
	return m, nil
}

func RegisterMethodHandler(name Verb, mh MethodHandler) {
	s, exists := signatures[name]
	if !exists {
		panic(fmt.Errorf("unknown method %s", name))
	}
	methods[name] = &Method{
		Name:      name,
		Signature: s,
		ActionE:   mh,
	}
}

// CallLocalE dispatches a verb to its registered handler in-process, decoding
// rawParams according to the verb signature. It mirrors what the MQTT RPC
// server does for remote requests, and is used by daemon-hosted scripts
// (MyHome.call) to reach Go infrastructure without a network round-trip.
func CallLocalE(ctx context.Context, verb Verb, rawParams json.RawMessage) (any, error) {
	m, err := Methods(verb)
	if err != nil {
		return nil, err
	}
	params := m.Signature.NewParams()
	if len(rawParams) > 0 && string(rawParams) != "null" {
		if params != nil && reflect.ValueOf(params).Kind() == reflect.Pointer {
			if err := json.Unmarshal(rawParams, params); err != nil {
				return nil, fmt.Errorf("invalid params for %s: %w", verb, err)
			}
		} else {
			if err := json.Unmarshal(rawParams, &params); err != nil {
				return nil, fmt.Errorf("invalid params for %s: %w", verb, err)
			}
		}
	}
	return m.ActionE(ctx, params)
}

var methods map[Verb]*Method = make(map[Verb]*Method)

var signatures map[Verb]MethodSignature = map[Verb]MethodSignature{
	DevicesMatch: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return &[]DeviceSummary{}
		},
	},
	DeviceLookup: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return &[]DeviceSummary{}
		},
	},
	DeviceShow: {
		NewParams: func() any {
			return &DeviceShowParams{}
		},
		NewResult: func() any {
			return &Device{}
		},
	},
	DeviceForget: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return nil
		},
	},
	DeviceRefresh: {
		NewParams: func() any {
			return "" // device identifier (id/name/host/etc)
		},
		NewResult: func() any {
			return nil
		},
	},
	DeviceSetup: {
		NewParams: func() any {
			return &DeviceSetupParams{}
		},
		NewResult: func() any {
			return nil
		},
	},
	DeviceUpdate: {
		NewParams: func() any {
			return &Device{}
		},
		NewResult: func() any {
			return nil
		},
	},
	TemperatureGet: {
		NewParams: func() any {
			return &TemperatureGetParams{}
		},
		NewResult: func() any {
			return &TemperatureRoomConfig{}
		},
	},
	TemperatureSet: {
		NewParams: func() any {
			return &TemperatureSetParams{}
		},
		NewResult: func() any {
			return &TemperatureSetResult{}
		},
	},
	TemperatureList: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &TemperatureRoomList{}
		},
	},
	TemperatureDelete: {
		NewParams: func() any {
			return &TemperatureDeleteParams{}
		},
		NewResult: func() any {
			return &TemperatureDeleteResult{}
		},
	},
	TemperatureGetSchedule: {
		NewParams: func() any {
			return &TemperatureGetScheduleParams{}
		},
		NewResult: func() any {
			return &TemperatureScheduleResult{}
		},
	},
	TemperatureGetWeekdayDefaults: {
		NewParams: func() any {
			return &TemperatureGetWeekdayDefaultsParams{}
		},
		NewResult: func() any {
			return &TemperatureWeekdayDefaults{}
		},
	},
	TemperatureSetWeekdayDefault: {
		NewParams: func() any {
			return &TemperatureSetWeekdayDefaultParams{}
		},
		NewResult: func() any {
			return &TemperatureSetWeekdayDefaultResult{}
		},
	},
	TemperatureGetKindSchedules: {
		NewParams: func() any {
			return &TemperatureGetKindSchedulesParams{}
		},
		NewResult: func() any {
			return &TemperatureKindScheduleList{}
		},
	},
	TemperatureSetKindSchedule: {
		NewParams: func() any {
			return &TemperatureSetKindScheduleParams{}
		},
		NewResult: func() any {
			return &TemperatureSetKindScheduleResult{}
		},
	},
	OccupancyGetStatus: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &OccupancyStatusResult{}
		},
	},
	HeaterGetConfig: {
		NewParams: func() any {
			return &HeaterGetConfigParams{}
		},
		NewResult: func() any {
			return &HeaterGetConfigResult{}
		},
	},
	HeaterSetConfig: {
		NewParams: func() any {
			return &HeaterSetConfigParams{}
		},
		NewResult: func() any {
			return &HeaterSetConfigResult{}
		},
	},
	ThermometerList: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &ThermometerListResult{}
		},
	},
	DoorList: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &DoorListResult{}
		},
	},
	RoomList: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &RoomListResult{}
		},
	},
	RoomCreate: {
		NewParams: func() any {
			return &RoomCreateParams{}
		},
		NewResult: func() any {
			return &RoomCreateResult{}
		},
	},
	RoomEdit: {
		NewParams: func() any {
			return &RoomEditParams{}
		},
		NewResult: func() any {
			return &RoomEditResult{}
		},
	},
	RoomDelete: {
		NewParams: func() any {
			return &RoomDeleteParams{}
		},
		NewResult: func() any {
			return &RoomDeleteResult{}
		},
	},
	DeviceSetRoom: {
		NewParams: func() any {
			return &DeviceSetRoomParams{}
		},
		NewResult: func() any {
			return nil
		},
	},
	DeviceListByRoom: {
		NewParams: func() any {
			return &DeviceListByRoomParams{}
		},
		NewResult: func() any {
			return &DeviceListByRoomResult{}
		},
	},
	SwitchToggle: {
		NewParams: func() any {
			return &SwitchParams{}
		},
		NewResult: func() any {
			return &SwitchResult{}
		},
	},
	SwitchOn: {
		NewParams: func() any {
			return &SwitchParams{}
		},
		NewResult: func() any {
			return &SwitchResult{}
		},
	},
	SwitchOff: {
		NewParams: func() any {
			return &SwitchParams{}
		},
		NewResult: func() any {
			return &SwitchResult{}
		},
	},
	SwitchStatus: {
		NewParams: func() any {
			return &SwitchParams{}
		},
		NewResult: func() any {
			return &SwitchResult{}
		},
	},
	SwitchAll: {
		NewParams: func() any {
			return &SwitchAllParams{}
		},
		NewResult: func() any {
			return &SwitchAllResult{}
		},
	},
	EventList: {
		NewParams: func() any {
			return &EventListRequest{}
		},
		NewResult: func() any {
			return &EventListResponse{}
		},
	},
	ScriptInvoke: {
		NewParams: func() any {
			return &ScriptInvokeParams{}
		},
		NewResult: func() any {
			return &ScriptInvokeResult{}
		},
	},
	LanHosts: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &LanHostsResult{}
		},
	},
}
