package script

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/asnowfix/home-automation/internal/shelly/scripts"

	"github.com/go-logr/logr"
)

// LoadScript returns the source code for a device script named name,
// preferring a same-named file in dir over the copy embedded in the myhome
// binary, and reports which source won ("local" or "embedded").
//
// dir == "" disables local loading outright: LoadScript always resolves to
// the embedded copy, so an empty or unset directory changes nothing. This is
// the default everywhere — see the --local-scripts-dir / --no-local-scripts
// flags on `myhome ctl shelly script upload|update` — specifically so a
// stale file lying around a working directory (a developer's checkout, or
// the daemon's working directory on the NAS) can never silently shadow the
// shipped script (issue #457, "Production must not pick up strays").
//
// name must be a flat file name: no path separators, no "." or "..". This
// keeps resolution unambiguous (exactly one candidate path) and rules out
// path traversal outside dir.
//
// Every resolution is logged at INFO with the path and the winning source —
// this is the safety net a caller relies on to notice a stale local file
// before trusting it, per the issue's requirement that source be logged
// "both path and which source won" on every call.
//
// LoadScript does not change how the returned code is used: it is handed to
// UploadWithVersion exactly as an embedded read would be, so version
// tracking (SHA1 of the code, recorded in device KVS) keeps working
// unmodified — a locally-loaded script simply hashes to its own version.
func LoadScript(log logr.Logger, dir, name string) (code []byte, source string, err error) {
	if err := validateFlatScriptName(name); err != nil {
		return nil, "", err
	}

	if dir != "" {
		path := filepath.Join(dir, name)
		buf, readErr := os.ReadFile(path)
		if readErr == nil {
			log.Info("Resolved script source", "name", name, "source", "local", "path", path)
			return buf, "local", nil
		}
		if !errors.Is(readErr, fs.ErrNotExist) {
			// A real error (permissions, a directory of the same name, ...)
			// deserves a louder note before falling back — a misconfigured
			// --local-scripts-dir should not look identical to "not present".
			log.Info("Local script read failed, falling back to embedded copy", "name", name, "path", path, "error", readErr.Error())
		}
	}

	buf, err := fs.ReadFile(scripts.GetFS(), name)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read script %s: %w", name, err)
	}
	log.Info("Resolved script source", "name", name, "source", "embedded")
	return buf, "embedded", nil
}

// validateFlatScriptName rejects anything but a bare file name: no
// subdirectories, no path traversal. "pool-pump.js" is fine; "sub/x.js",
// "../x.js", ".", and ".." are all rejected.
func validateFlatScriptName(name string) error {
	if name == "" {
		return fmt.Errorf("empty script name")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid script name %q", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("script name must be a flat file name, not a path: %q", name)
	}
	return nil
}
