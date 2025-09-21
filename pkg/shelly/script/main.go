package script

import (
	"bytes"
	"context"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"fmt"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"reflect"
	"strconv"

	"github.com/tdewolff/minify/v2"
	mjs "github.com/tdewolff/minify/v2/js"
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

func minifyJS(src []byte) ([]byte, error) {
	m := minify.New()
	m.AddFunc("text/javascript", mjs.Minify)
	var out bytes.Buffer
	if err := m.Minify("text/javascript", &out, bytes.NewReader(src)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// downgradeTemplates converts ES6 template literals without interpolations (${...})
// into normal double-quoted strings with escaped newlines and quotes. This helps
// older JS engines that don't support backtick template strings.
func downgradeTemplates(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inStr := false
	strQuote := byte(0)
	esc := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(src); i++ {
		b := src[i]

		// Handle existing string/comment contexts
		if inStr {
			out = append(out, b)
			if esc {
				esc = false
			} else if b == '\\' {
				esc = true
			} else if b == strQuote {
				inStr = false
			}
			continue
		}
		if inLineComment {
			out = append(out, b)
			if b == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			out = append(out, b)
			if b == '*' && i+1 < len(src) && src[i+1] == '/' {
				out = append(out, '/')
				i++
				inBlockComment = false
			}
			continue
		}

		// Entering comment?
		if b == '/' && i+1 < len(src) {
			if src[i+1] == '/' {
				inLineComment = true
				out = append(out, b, src[i+1])
				i++
				continue
			} else if src[i+1] == '*' {
				inBlockComment = true
				out = append(out, b, src[i+1])
				i++
				continue
			}
		}

		// Entering normal quote string?
		if b == '\'' || b == '"' {
			inStr = true
			strQuote = b
			out = append(out, b)
			continue
		}

		// Handle template literal starting with backtick
		if b == '`' {
			// Scan until matching backtick; if we see an interpolation (${), abort conversion
			j := i + 1
			hasInterp := false
			esc2 := false
			for ; j < len(src); j++ {
				c := src[j]
				if esc2 {
					esc2 = false
					continue
				}
				if c == '\\' {
					esc2 = true
					continue
				}
				if c == '$' && j+1 < len(src) && src[j+1] == '{' {
					hasInterp = true
					break
				}
				if c == '`' {
					break
				}
			}
			if j < len(src) && src[j] == '`' && !hasInterp {
				// Convert content src[i+1:j] to a quoted string
				out = append(out, '"')
				for k := i + 1; k < j; k++ {
					ch := src[k]
					switch ch {
					case '\\':
						out = append(out, '\\', '\\')
					case '"':
						out = append(out, '\\', '"')
					case '\n':
						out = append(out, '\\', 'n')
					case '\r':
						out = append(out, '\\', 'r')
					case '\t':
						out = append(out, '\\', 't')
					default:
						out = append(out, ch)
					}
				}
				out = append(out, '"')
				i = j // skip to closing backtick
				continue
			}
			// Otherwise, emit as-is and continue
			out = append(out, b)
			continue
		}

		// Default
		out = append(out, b)
	}

	return out
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

func ScriptStatus(ctx context.Context, device types.Device, via types.Channel, name string) (*Status, error) {
	id, err := isLoaded(ctx, via, device, name)
	if err != nil {
		return nil, err
	}
	out, err := device.CallE(ctx, via, string(GetStatus), &StatusStartStopDeleteRequest{Id: id})
	if err != nil {
		log.Error(err, "Unable to get script status", "id", id, "name", name)
		return nil, err
	}
	status, ok := out.(*Status)
	if !ok {
		err := fmt.Errorf("unexpected format '%v' (failed to cast response into script.Status)", reflect.TypeOf(out))
		log.Error(err, "Unable to get script status", "id", id, "name", name)
		return nil, err
	}
	return status, nil
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

	out, err := device.CallE(ctx, via, string(operation), &StatusStartStopDeleteRequest{Id: id})
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

func Upload(ctx context.Context, via types.Channel, device types.Device, name string, minify bool) (uint32, error) {
	buf, err := content.ReadFile(name)
	if err != nil {
		log.Error(err, "Unknown script", "name", name)
		return 0, err
	}

	// Compute version as the sha1 checksum of the script before its minification
	h := sha1.New()
	h.Write(buf)
	version := hex.EncodeToString(h.Sum(nil))

	// read the scrip version from the KVS
	kvsKey := fmt.Sprintf("script/%s", name)
	kvsVersion := ""
	out, err := kvs.GetValue(ctx, log, via, device, kvsKey)
	if err != nil || out == nil {
		log.Error(err, "Unable to get KVS entry for script version", "key", kvsKey)
		// Don't fail the upload if KVS fails, just log the error
	} else {
		kvsVersion = out.Value
		log.Info("Got KVS entry for script version", "key", kvsKey, "version", kvsVersion)
	}

	var id uint32
	if version != kvsVersion {
		log.Info("Script version is different, uploading new one", "name", name, "version", version)
		id, err = doUpload(ctx, via, device, name, buf, minify, kvsKey, version)
		if err != nil {
			return 0, err
		}
	} else {
		log.Info("Script version is the same, skipping upload", "name", name, "version", version)
	}
	_, err = StartStopDelete(ctx, via, device, name, Start)
	if err != nil {
		log.Error(err, "Unable to start script", "name", name)
		return 0, err
	}
	return id, nil
}

func doUpload(ctx context.Context, via types.Channel, device types.Device, name string, buf []byte, minify bool, versionKey string, version string) (uint32, error) {
	// Minify before splitting and uploading (only if requested)
	if minify {
		origLen := len(buf)
		if minified, err := minifyJS(buf); err != nil {
			log.Error(err, "Minify failed", "name", name)
			return 0, err
		} else {
			buf = minified
			log.Info("Minified script", "name", name, "from", origLen, "to", len(buf))
			// Downgrade ES6 template literals without interpolations to plain strings
			before := len(buf)
			buf = downgradeTemplates(buf)
			if len(buf) != before {
				log.Info("Downgraded template literals in minified script", "name", name)
			}
		}
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
		out, err := device.CallE(ctx, via, Stop.String(), &StatusStartStopDeleteRequest{Id: id})
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

	// Create/update KVS entry with script version
	_, err = kvs.SetKeyValue(ctx, log, via, device, versionKey, version)
	if err != nil {
		log.Error(err, "Unable to set KVS entry for script version", "key", versionKey, "version", version)
		// Don't fail the upload if KVS fails, just log the error
	} else {
		log.Info("Set KVS entry for script version", "key", versionKey, "version", version)
	}

	return id, nil
}
