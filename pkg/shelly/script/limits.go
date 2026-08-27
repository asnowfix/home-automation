package script

import "fmt"

// MaxSourceBytes is the Shelly Gen2 firmware's script source-length limit, in
// bytes: Script.PutCode rejects (out_of_codespace, see the Errors list in
// types.go) any script whose total code length — after any append chunks are
// stitched together — exceeds this.
//
// Value: 65535. This is the number reported in issue #553, corroborated by
// this repo's own observation there that an unminified
// internal/shelly/scripts/pool-pump.js (~118 KB at the time) was rejected by
// a real device (`mezzanine`, 2026-08-25) while the minified build (~34.6 KB)
// was accepted. It has NOT been independently re-verified against firmware
// (Shelly.GetDeviceInfo/Script.GetConfig expose no such field as of this
// writing) or against the official docs
// (https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Script)
// from within this environment, since neither live-device RPC nor web access
// was available for that check when this constant was added — see #553.
// Whether it is universal across Gen2 models (Plus1, Pro3, ...) or varies by
// flash size/profile is unconfirmed. If a device is ever found to accept or
// reject a different length, correct the value here — this is the only
// place it is defined.
const MaxSourceBytes = 65535

// ScriptTooLargeError is returned before any upload RPC is attempted when a
// script's source exceeds MaxSourceBytes, so the failure is loud and
// immediate — naming the limit and the actual size — rather than an opaque
// device-side rejection (or, per #538, a silently-swallowed one) well after
// the operator believes the upload started. See issue #553.
type ScriptTooLargeError struct {
	Name            string
	Size            int
	Limit           int
	MinifyRequested bool
}

func (e *ScriptTooLargeError) Error() string {
	if !e.MinifyRequested {
		return fmt.Sprintf(
			"script %q is %d bytes, over the device's %d-byte script-source limit — --no-minify cannot work for this script; upload without --no-minify (the code will be minified before it is sent) or shrink the script",
			e.Name, e.Size, e.Limit,
		)
	}
	return fmt.Sprintf(
		"script %q is %d bytes after minification, still over the device's %d-byte script-source limit — the script must be shrunk before it can be uploaded",
		e.Name, e.Size, e.Limit,
	)
}

// checkSourceLength rejects code that would be sent to the device over
// MaxSourceBytes. It must be called with the code exactly as it will be
// transmitted — i.e. after any minification has already been applied — so
// that minifyRequested reflects what the caller actually asked for (and thus
// whether "--no-minify cannot work" is the correct message).
func checkSourceLength(name string, code []byte, minifyRequested bool) error {
	if len(code) > MaxSourceBytes {
		return &ScriptTooLargeError{
			Name:            name,
			Size:            len(code),
			Limit:           MaxSourceBytes,
			MinifyRequested: minifyRequested,
		}
	}
	return nil
}
