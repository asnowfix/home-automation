package script

import (
	"context"
	"embed"
	"fmt"
	"pkg/shelly/types"
	"reflect"
	"strconv"
)

//go:embed *.js
var content embed.FS

func ListAvailable() ([]string, error) {
	dir, err := content.ReadDir(".")
	if err != nil {
		log.Error(err, "Unable to list embedded scripts")
		return nil, err
	}

	scripts := make([]string, 0)
	for _, entry := range dir {
		if !entry.IsDir() {
			scripts = append(scripts, entry.Name())
		}
	}

	return scripts, nil
}

func ListLoaded(ctx context.Context, via types.Channel, device types.Device) ([]Status, error) {
	out, err := device.CallE(ctx, via, string(List), nil)
	if err != nil {
		log.Error(err, "Unable to list scripts")
		return nil, err
	}
	return out.(*ListResponse).Scripts, nil
}

func isLoaded(ctx context.Context, via types.Channel, device types.Device, name string) (uint32, error) {
	id, err := strconv.Atoi(name)
	if err == nil {
		return uint32(id), nil
	}

	loaded, err := ListLoaded(ctx, via, device)
	if err != nil {
		return 0, err
	}

	for _, l := range loaded {
		if l.Name == name {
			return uint32(l.Id), nil
		}
		if id != 0 && l.Id == uint32(id) {
			return uint32(l.Id), nil
		}
	}

	return 0, fmt.Errorf("script not found: name=%v id=%v", name, id)
}

func DeviceStatus(ctx context.Context, device types.Device, via types.Channel) ([]Status, error) {
	available, err := ListAvailable()
	if err != nil {
		return nil, err
	}

	loaded, err := ListLoaded(ctx, via, device)
	if err != nil {
		return nil, err
	}

	status := make([]Status, 0)

	for _, l := range loaded {
		s := Status{
			Id:      l.Id,
			Name:    l.Name,
			Running: l.Running,
			Manual:  true,
		}
		for _, name := range available {
			if l.Name == name {
				s.Manual = false
				break
			}
		}
		status = append(status, s)
	}

	return status, nil
}

func StartStopDelete(ctx context.Context, via types.Channel, device types.Device, name string, operation Verb) (any, error) {
	id, err := isLoaded(ctx, via, device, name)
	if err != nil {
		log.Error(err, "Did not find loaded script", "name", name)
		return nil, err
	}

	out, err := device.CallE(ctx, via, string(operation), &StartStopDeleteRequest{Id: id})
	if err != nil {
		log.Error(err, "Unable to run on script", "id", id, "operation", operation)
		return nil, err
	}
	return out, nil
}

func EnableDisable(ctx context.Context, via types.Channel, device types.Device, name string, enable bool) (any, error) {
	id, err := isLoaded(ctx, via, device, name)
	if err != nil {
		log.Error(err, "Did not find loaded script", "name", name)
		return nil, err
	}
	out, err := device.CallE(ctx, via, string(SetConfig), &ConfigurationRequest{
		Id: id,
		Configuration: Configuration{
			Id:     id,
			Name:   name,
			Enable: enable,
		},
	})
	if err != nil {
		log.Error(err, "Unable to configure script", "id", id, "name", name)
		return nil, err
	}
	return out, nil
}

func Download(ctx context.Context, via types.Channel, device types.Device, name string, id uint32) (string, error) {
	out, err := device.CallE(ctx, via, string(GetCode), &GetCodeRequest{
		Id: id,
	})
	if err != nil {
		log.Error(err, "Unable to get code", "id", id, "name", name)
		return "", err
	}
	res, ok := out.(*GetCodeResponse)
	if !ok {
		return "", fmt.Errorf("unexpected format '%v' (failed to cast response)", reflect.TypeOf(out))
	}
	return res.Data, nil
}

func Upload(ctx context.Context, via types.Channel, device types.Device, name string) (uint32, error) {

	buf, err := content.ReadFile(name)
	if err != nil {
		log.Error(err, "Unknown script", "name", name)
		return 0, err
	}

	id, err := isLoaded(ctx, via, device, name)
	if err != nil {
		// Script not loaded: create a new one
		out, err := device.CallE(ctx, via, string(Create), &Configuration{
			Name:   name,
			Enable: true,
		})
		if err != nil {
			return 0, err
		}
		status, ok := out.(*Status)
		if !ok {
			return 0, fmt.Errorf("unexpected format (failed to cast status)")
		}

		id = status.Id
		log.Info("Created script", "name", name, "id", id)
	} else {
		// script loaded: stop it, in case it is running
		out, err := device.CallE(ctx, via, Stop.String(), &StartStopDeleteRequest{Id: id})
		if err != nil {
			log.Error(err, "Unable to stop script", "id", id, "name", name)
			return 0, err
		}
		log.Info("Stopped script", "name", name, "id", id, "out", out)
	}

	// upload chunks of 2048
	append := false // first chunk is a replacement
	chunkSize := 2048
	for i := 0; i < len(buf); i += chunkSize {
		end := i + chunkSize
		if end > len(buf) {
			end = len(buf)
		}
		chunk := buf[i:end]
		out, err := device.CallE(ctx, via, string(PutCode), &PutCodeRequest{
			Id:     id,
			Code:   string(chunk),
			Append: append,
		})
		if err != nil {
			log.Error(err, "Unable to upload script", "id", id, "name", name, "index", i)
			return 0, err
		}
		log.Info("Uploaded script chunk", "name", name, "id", id, "index", i, "out", out)
		append = true
	}
	log.Info("Uploaded script", "name", name, "id", id)

	// enable: auto-start at next reboot
	out, err := device.CallE(ctx, via, string(SetConfig), &ConfigurationRequest{
		Id: id,
		Configuration: Configuration{
			Id:     id,
			Enable: true,
		},
	})
	if err != nil {
		log.Error(err, "Unable to configure script", "id", id, "name", name)
		return 0, err
	}
	log.Info("Configured script", "name", name, "id", id, "out", out)

	// start now
	out, err = device.CallE(ctx, via, string(Start), &StartStopDeleteRequest{Id: id})
	if err != nil {
		log.Error(err, "Unable to start script", "id", id, "name", name)
		return 0, err
	}
	log.Info("Started script", "name", name, "id", id, "out", out)

	return id, nil
}
