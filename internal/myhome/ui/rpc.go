package ui

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/go-logr/logr"
)

func RpcHandler(ctx context.Context, log logr.Logger, registry *myhome.Registry) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method myhome.Verb     `json:"method"`
			Params json.RawMessage `json:"params"`
		}

		log.Info("RPC request received", "remote", r.RemoteAddr, "content-length", r.ContentLength)

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Lookup method
		mh, err := registry.Methods(req.Method)
		if err != nil {
			log.Error(err, "method not found", "method", req.Method)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// mh.Action decodes req.Params directly into the handler's declared
		// parameter type — exactly the same raw-JSON entry point the server
		// uses for a wire request, so there is no separate NewParams/Unmarshal
		// step here anymore.
		res, err := mh.Action(ctx, req.Params)
		if err != nil {
			log.Error(err, "method failed", "method", req.Method)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Convert device.show result to DeviceView for the UI
		if mh.Name == myhome.DeviceShow {
			if device, ok := res.(*myhome.Device); ok && device != nil {
				res = DeviceToView(ctx, device)
			}
		}

		// Convert device.lookup & device.listbyroom result to DeviceView for the UI
		if mh.Name == myhome.DeviceLookup || mh.Name == myhome.DeviceListByRoom {
			if devices, ok := res.([]myhome.Device); ok {
				views := make([]DeviceView, len(devices))
				for i, device := range devices {
					views[i] = DeviceToView(ctx, &device)
				}
				res = views
			}
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": res})
	}
}
