package myhome

import (
	"context"
	"fmt"
	"pkg/devices"
)

var TheClient Client

type Client interface {
	LookupDevices(ctx context.Context, name string) (*[]devices.Device, error)
	ForgetDevices(ctx context.Context, name string) error
	CallE(ctx context.Context, method Verb, params any) (any, error)
	Shutdown()
}

func ServerTopic() string {
	return fmt.Sprintf("%s/rpc", MYHOME)
}

func ClientTopic(clientId string) string {
	return fmt.Sprintf("%s/rpc", clientId)
}

type Dialog struct {
	Id  string `json:"id"`
	Src string `json:"src"`
	Dst string `json:"dst"`
}

type request struct {
	Dialog
	Method Verb `json:"method"`
	Params any  `json:"params,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	Dialog
	Result *any   `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

func ValidateDialog(d Dialog) error {
	if d.Id == "" {
		return fmt.Errorf("invalid dialog: id=%v", d.Id)
	}
	if d.Src == "" {
		return fmt.Errorf("invalid dialog: src=%v", d.Src)
	}
	if d.Dst == "" {
		return fmt.Errorf("invalid dialog: dst=%v", d.Dst)
	}
	return nil
}
