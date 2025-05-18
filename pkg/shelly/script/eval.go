package script

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"pkg/shelly/types"
)

func EvalInDevice(ctx context.Context, via types.Channel, device types.Device, name string, code string) (any, error) {
	log := hlog.Logger
	id, err := isLoaded(ctx, via, device, name)
	if err != nil {
		log.Error(err, "Did not find loaded script", "name", name)
		return nil, err
	}
	out, err := device.CallE(ctx, via, string(Eval), &EvalRequest{
		Id:   id,
		Code: code,
	})
	if err != nil {
		log.Error(err, "Unable to eval script", "id", id)
		return nil, err
	}
	response := out.(*EvalResponse)
	s, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return response, nil
}
