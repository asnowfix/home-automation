package script

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/dop251/goja"
)

// installUtilities adds the top-level Shelly scripting utility functions:
// https://shelly-api-docs.shelly.cloud/gen2/Scripts/APIs/Utilities
//
// Shelly scripts treat JS strings as byte strings (each character code is a
// byte 0-255), matching the browser btoa/atob convention — not UTF-8 text.
func installUtilities(vm *goja.Runtime) {
	vm.Set("btoa", func(call goja.FunctionCall) goja.Value {
		raw, err := stringToBytes(call.Argument(0).String())
		if err != nil {
			panic(vm.ToValue(fmt.Sprintf("btoa: %v", err)))
		}
		return vm.ToValue(base64.StdEncoding.EncodeToString(raw))
	})

	vm.Set("atob", func(call goja.FunctionCall) goja.Value {
		raw, err := base64.StdEncoding.DecodeString(call.Argument(0).String())
		if err != nil {
			panic(vm.ToValue(fmt.Sprintf("atob: invalid base64 input: %v", err)))
		}
		return vm.ToValue(bytesToString(raw))
	})

	vm.Set("btoh", func(call goja.FunctionCall) goja.Value {
		raw, err := stringToBytes(call.Argument(0).String())
		if err != nil {
			panic(vm.ToValue(fmt.Sprintf("btoh: %v", err)))
		}
		return vm.ToValue(hex.EncodeToString(raw))
	})
}

// stringToBytes converts a JS byte-string (each rune in 0-255) to raw bytes.
func stringToBytes(s string) ([]byte, error) {
	raw := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return nil, fmt.Errorf("character code %d is out of byte range (0-255)", r)
		}
		raw = append(raw, byte(r))
	}
	return raw, nil
}

// bytesToString converts raw bytes back to a JS byte-string.
func bytesToString(raw []byte) string {
	runes := make([]rune, len(raw))
	for i, b := range raw {
		runes[i] = rune(b)
	}
	return string(runes)
}
