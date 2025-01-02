package script

import (
	"devices/shelly/types"
	"embed"
	"fmt"
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

func listLoaded(via types.Channel, device types.Device) ([]Status, error) {
	out, err := device.CallE(via, "Script", "List", nil)
	if err != nil {
		log.Error(err, "Unable to list scripts")
		return nil, err
	}
	return out.(*ListResponse).Scripts, nil
}

func isLoaded(via types.Channel, device types.Device, name string, id uint32) (*Id, error) {
	loaded, err := listLoaded(via, device)
	if err != nil {
		return nil, err
	}

	for _, l := range loaded {
		if l.Name == name {
			return &l.Id, nil
		}
	}

	return nil, fmt.Errorf("script not found: name=%v", name)
}

func Load(device types.Device, via types.Channel, name string, autostart bool) (*Id, error) {
	buf, err := content.ReadFile(name)
	if err != nil {
		log.Error(err, "Unable to find script", "name", name)
		return nil, err
	}

	id, err := isLoaded(via, device, name, 0)
	if err != nil {
		out, err := device.CallE(via, "Script", "Create", &Configuration{
			Name:   name,
			Enable: autostart,
		})
		if err != nil {
			return nil, err
		}

		status := out.(*Status)
		if err != nil {
			return nil, err
		}

		id = &status.Id
		log.Info("Created script", "name", name, "id", id.Id)
	}

	out, err := device.CallE(via, "Script", "PutCode", &PutCodeRequest{
		Id:   *id,
		Code: string(buf),
	})
	if err != nil {
		return nil, err
	}
	log.Info("Updated script", "name", name, "id", id.Id, "out", out)
	return id, nil
}

func List(device types.Device, via types.Channel) ([]Status, error) {
	available, err := listAvailable()
	if err != nil {
		return nil, err
	}

	loaded, err := listLoaded(via, device)
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

func StartStopDelete(via types.Channel, device types.Device, name string, id uint32, operation string) (any, error) {
	sid, err := isLoaded(via, device, name, id)
	if err != nil {
		log.Error(err, "Did not find loaded script", "id", id, "name", name)
		return nil, err
	}

	out, err := device.CallE(via, "Script", operation, sid)
	if err != nil {
		log.Error(err, "Unable to run on script", "id", id, "operation", operation)
		return nil, err
	}
	return out, nil
}

func EnableDisable(via types.Channel, device types.Device, name string, id uint32, enable bool) (any, error) {
	sid, err := isLoaded(via, device, name, id)
	if err != nil {
		// Did not find the named script, create it
		out, err := device.CallE(via, "Script", "Create", &Configuration{
			Name:   name,
			Enable: enable,
		})
		if err != nil {
			log.Error(err, "Unable to create script", "name", name)
			return nil, err
		}
		return out, nil
	} else {
		out, err := device.CallE(via, "Script", "SetConfig", &ConfigurationRequest{
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
