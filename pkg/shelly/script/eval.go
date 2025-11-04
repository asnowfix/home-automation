package script

import (
	"context"
	"encoding/json"
	"fmt"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

func EvalInDevice(ctx context.Context, via types.Channel, device types.Device, name string, code string) (any, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic(err)
	}
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
		log.Error(err, "Script eval failed", "id", id)
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
