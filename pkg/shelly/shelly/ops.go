package shelly

import (
	"context"
	"fmt"
	"net/http"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"
	"time"

	"github.com/go-logr/logr"
)

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly>

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	GetStatus        Verb = "Shelly.GetStatus"
	GetConfig        Verb = "Shelly.GetConfig"
	ListMethods      Verb = "Shelly.ListMethods"
	getDeviceInfo    Verb = "Shelly.GetDeviceInfo"
	ListProfiles     Verb = "Shelly.ListProfiles"
	SetProfile       Verb = "Shelly.SetProfile"
	ListTimezones    Verb = "Shelly.ListTimezones"
	DetectLocation   Verb = "Shelly.DetectLocation"
	CheckForUpdate   Verb = "Shelly.CheckForUpdate"
	Update           Verb = "Shelly.Update"
	FactoryReset     Verb = "Shelly.FactoryReset"
	ResetWiFiConfig  Verb = "Shelly.ResetWiFiConfig"
	Reboot           Verb = "Shelly.Reboot"
	SetAuth          Verb = "Shelly.SetAuth"
	PutUserCA        Verb = "Shelly.PutUserCA"
	PutTLSClientCert Verb = "Shelly.PutTLSClientCert"
	PutTLSClientKey  Verb = "Shelly.PutTLSClientKey"
	GetComponents    Verb = "Shelly.GetComponents"
)

func Init(log logr.Logger, r types.MethodsRegistrar, timeout time.Duration) {
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(ListMethods.String(), types.MethodHandler{
		Allocate:   func() any { return new(MethodsResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(getDeviceInfo.String(), types.MethodHandler{
		Allocate:   func() any { return new(DeviceInfo) },
		HttpMethod: http.MethodGet,
	})

	// TODO complete the list of handlers

	r.RegisterMethodHandler(GetComponents.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(ComponentsRequest{}),
		Allocate:   func() any { return new(ComponentsResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Reboot.String(), types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(CheckForUpdate.String(), types.MethodHandler{
		Allocate:   func() any { return new(CheckForUpdateResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Update.String(), types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(FactoryReset.String(), types.MethodHandler{
		Allocate: func() any { return nil },
		// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly#shellyfactoryreset-example
		HttpMethod: http.MethodPost,
	})

	// TODO complete the list of handlers

	r.RegisterDeviceCaller(types.ChannelDefault, func(ctx context.Context, d types.Device, mh types.MethodHandler, out any, params any) (any, error) {
		return nil, fmt.Errorf("not implemented")
	})

}

func DoGetComponents(ctx context.Context, d types.Device, req *ComponentsRequest) (*ComponentsResponse, error) {
	out, err := d.CallE(ctx, types.ChannelDefault, string(GetComponents.String()), req)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("nil reply")
	}
	res, ok := out.(*ComponentsResponse)
	if !ok {
		return nil, fmt.Errorf("invalid components type %T (should be *ComponentsResponse)", out)
	}
	if res.Total == 0 {
		return nil, fmt.Errorf("no components found")
	}
	return res, nil
}

// DoCheckForUpdate checks if firmware updates are available
func DoCheckForUpdate(ctx context.Context, via types.Channel, d types.Device) (*CheckForUpdateResponse, error) {
	out, err := d.CallE(ctx, via, CheckForUpdate.String(), nil)
	if err != nil {
		return nil, err
	}
	updateInfo, ok := out.(*CheckForUpdateResponse)
	if !ok {
		return nil, fmt.Errorf("invalid update info type %T (should be *CheckForUpdateResponse)", out)
	}
	return updateInfo, nil
}

// UpdateRequest represents the parameters for Shelly.Update
type UpdateRequest struct {
	Stage string `json:"stage"` // "stable" or "beta"
}

// DoUpdate initiates a firmware update
func DoUpdate(ctx context.Context, via types.Channel, d types.Device, stage string) error {
	req := UpdateRequest{Stage: stage}
	_, err := d.CallE(ctx, via, Update.String(), &req)
	return err
}

func DoReboot(ctx context.Context, d types.Device) error {
	_, err := d.CallE(ctx, types.ChannelDefault, string(Reboot.String()), nil)
	return err
}

func GetDeviceInfo(ctx context.Context, d types.Device, via types.Channel) (*DeviceInfo, error) {
	out, err := d.CallE(ctx, via, string(getDeviceInfo.String()), nil)
	if err != nil {
		return nil, err
	}
	info, ok := out.(*DeviceInfo)
	if !ok {
		return nil, fmt.Errorf("invalid device info type %T (should be *DeviceInfo)", out)
	}
	if info.Id == "" || len(info.MacAddress) == 0 {
		return nil, fmt.Errorf("invalid device info id:%s mac:%s", info.Id, info.MacAddress)
	}

	return info, nil
}

// $ curl http://192.168.1.47/rpc/Shelly.GetComponents | jq '.components | .[] | select(.key | startswith("switch")) | .status'
// {
//   "id": 0,
//   "source": "loopback",
//   "output": false,
//   "apower": 0.0,
//   "voltage": 228.7,
//   "freq": 49.9,
//   "current": 0.000,
//   "aenergy": {
//     "total": 3413.548,
//     "by_minute": [
//       0.000,
//       0.000,
//       0.000
//     ],
//     "minute_ts": 1771058580
//   },
//   "ret_aenergy": {
//     "total": 0.000,
//     "by_minute": [
//       0.000,
//       0.000,
//       0.000
//     ],
//     "minute_ts": 1771058580
//   },
//   "temperature": {
//     "tC": 32.1,
//     "tF": 89.7
//   }
// }

func GetSwitchesSummary(ctx context.Context, d types.Device) (map[int]SwitchSummary, error) {
	comps, err := DoGetComponents(ctx, d, &ComponentsRequest{
		Include: []string{"config", "status"},
		// Shelly devices have at most 4 switches (Shelly PRO 4)
		Keys: []string{"switch:0", "switch:1", "switch:2", "switch:3"},
	})
	if err != nil {
		return nil, err
	}
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	log.V(1).Info("GetSwitchesSummary", "components", comps)

	switches := make(map[int]SwitchSummary, comps.Total)

	for id, swc := range []*sswitch.Config{comps.Config.Switch0, comps.Config.Switch1, comps.Config.Switch2, comps.Config.Switch3} {
		if swc == nil {
			continue
		}
		if id != swc.Id {
			log.V(1).Info("GetSwitchesSummary", "id", id, "swc_id", swc.Id)
		}

		ss := SwitchSummary{
			Id: swc.Id,
		}
		if swc.Name != "" {
			ss.Name = swc.Name
		} else {
			ss.Name = fmt.Sprintf("switch:%d", swc.Id)
		}
		switches[id] = ss
	}

	for id, sws := range []*sswitch.Status{comps.Status.Switch0, comps.Status.Switch1, comps.Status.Switch2, comps.Status.Switch3} {
		if sws == nil {
			continue
		}
		ss := switches[id]
		ss.On = sws.Output
		switches[id] = ss
	}

	return switches, nil
}
