package ui

import (
	"context"
	"encoding/json"
	"myhome"
	"net/http"

	"github.com/go-logr/logr"
)

func RpcHandler(ctx context.Context, log logr.Logger) func(w http.ResponseWriter, r *http.Request) {
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
		mh, err := myhome.Methods(req.Method)
		if err != nil {
			log.Error(err, "method not found", "method", req.Method)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Decode params into the expected type for this method
		params := mh.Signature.NewParams()
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				log.Error(err, "invalid params", "method", req.Method)
				http.Error(w, "invalid params: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		// Call method
		res, err := mh.ActionE(ctx, params)
		if err != nil {
			log.Error(err, "method failed", "method", req.Method)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Convert device.show result to DeviceView for the UI
		if mh.Name == myhome.DeviceShow {
			if device, ok := res.(*myhome.Device); ok && device != nil {
				res = DeviceToView(device)
			}
		}

		// Convert device.lookup & device.listbyroom result to DeviceView for the UI
		if mh.Name == myhome.DeviceLookup || mh.Name == myhome.DeviceListByRoom {
			if devices, ok := res.([]myhome.Device); ok && devices != nil {
				res := make([]DeviceView, len(devices))
				for i, device := range devices {
					res[i] = DeviceToView(&device)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": res})
	}
}
