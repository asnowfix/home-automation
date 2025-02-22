package script

import (
	"context"
	"embed"
	"fmt"
	"pkg/shelly/types"
)

//go:embed *.js
var content embed.FS

func listAvailable() ([]string, error) {
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

func listLoaded(ctx context.Context, via types.Channel, device types.Device) ([]Status, error) {
	out, err := device.CallE(ctx, via, string(List), nil)
	if err != nil {
		log.Error(err, "Unable to list scripts")
		return nil, err
	}
	return out.(*ListResponse).Scripts, nil
}

func isLoaded(ctx context.Context, via types.Channel, device types.Device, name string, id uint32) (*Id, error) {
	loaded, err := listLoaded(ctx, via, device)
	if err != nil {
		return nil, err
	}

	for _, l := range loaded {
		if l.Name == name {
			return &l.Id, nil
		}
		if l.Id.Id == id {
			return &l.Id, nil
		}
	}

	return nil, fmt.Errorf("script not found: name=%v", name)
}

func Load(ctx context.Context, device types.Device, via types.Channel, name string, autostart bool) (*Id, error) {
	buf, err := content.ReadFile(name)
	if err != nil {
		log.Error(err, "Unable to find script", "name", name)
		return nil, err
	}

	id, err := isLoaded(ctx, via, device, name, 0)
	if err != nil {
		out, err := device.CallE(ctx, via, string(Create), &Configuration{
			Name:   name,
			Enable: autostart,
		})
		if err != nil {
			return nil, err
		}

		status, ok := out.(*Status)
		if !ok {
			return nil, fmt.Errorf("unexpected format (failed to cast status)")
		}

		id = &status.Id
		log.Info("Created script", "name", name, "id", id.Id)
	}

	out, err := device.CallE(ctx, via, string(PutCode), &PutCodeRequest{
		Id:   *id,
		Code: string(buf),
	})
	if err != nil {
		return nil, err
	}
	log.Info("Updated script", "name", name, "id", id.Id, "out", out)
	return id, nil
}

func ListAll(ctx context.Context, device types.Device, via types.Channel) ([]Status, error) {
	available, err := listAvailable()
	if err != nil {
		return nil, err
	}

	loaded, err := listLoaded(ctx, via, device)
	if err != nil {
		return nil, err
	}

	status := make([]Status, 0)
	for _, name := range available {
		isLoaded := false
		for _, l := range loaded {
			if l.Name == name {
				status = append(status, Status{
					Id:      l.Id,
					Name:    l.Name,
					Running: l.Running,
					Loaded:  true,
				})
				isLoaded = true
			}
		}
		if !isLoaded {
			status = append(status, Status{
				Id:     Id{Id: 0},
				Name:   name,
				Loaded: false,
			})
		}
	}

	return status, nil
}

func StartStopDelete(ctx context.Context, via types.Channel, device types.Device, name string, id uint32, operation Verb) (any, error) {
	sid, err := isLoaded(ctx, via, device, name, id)
	if err != nil {
		log.Error(err, "Did not find loaded script", "id", id, "name", name)
		return nil, err
	}

	out, err := device.CallE(ctx, via, string(operation), sid)
	if err != nil {
		log.Error(err, "Unable to run on script", "id", id, "operation", operation)
		return nil, err
	}
	return out, nil
}

func EnableDisable(ctx context.Context, via types.Channel, device types.Device, name string, id uint32, enable bool) (any, error) {
	sid, err := isLoaded(ctx, via, device, name, id)
	if err != nil {
		// Did not find the named script, create it
		out, err := device.CallE(ctx, via, string(Create), &Configuration{
			Name:   name,
			Enable: enable,
		})
		if err != nil {
			log.Error(err, "Unable to create script", "name", name)
			return nil, err
		}
		return out, nil
	} else {
		out, err := device.CallE(ctx, via, string(SetConfig), &ConfigurationRequest{
			Id: *sid,
			Configuration: Configuration{
				Id:     *sid,
				Name:   name,
				Enable: enable,
			},
		})
		if err != nil {
			log.Error(err, "Unable to configure script", "id", id)
			return nil, err
		}
		return out, nil
	}
}
