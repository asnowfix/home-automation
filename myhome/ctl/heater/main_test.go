package heater

import (
	"io/fs"
	"shelly/scripts"
	"testing"

	"github.com/dop251/goja"
)

// TestHeaterKVSKeysMatchJSSchema validates that heaterKVSKeys in main.go
// matches CONFIG_SCHEMA in heater.js
func TestHeaterKVSKeysMatchJSSchema(t *testing.T) {
	// Read heater.js from embedded filesystem
	scriptFS := scripts.GetFS()
	content, err := fs.ReadFile(scriptFS, "heater.js")
	if err != nil {
		t.Fatalf("Failed to read heater.js from embedded FS: %v", err)
	}

	// Parse CONFIG_SCHEMA from JavaScript using goja
	schemaKeys := parseConfigSchema(t, string(content))

	// Build expected KVS keys from schema
	expectedKeys := make(map[string]bool)
	for jsField, schemaEntry := range schemaKeys {
		kvsKey := schemaEntry.Key
		if !schemaEntry.Unprefixed {
			kvsKey = "script/heater/" + kvsKey
		}
		expectedKeys[kvsKey] = true

		t.Logf("Schema field %s -> KVS key %s", jsField, kvsKey)
	}

	// Verify heaterKVSKeys matches expected keys
	actualKeys := make(map[string]bool)
	for _, key := range heaterKVSKeys {
		actualKeys[key] = true
	}

	// Check for missing keys in heaterKVSKeys
	for expectedKey := range expectedKeys {
		if !actualKeys[expectedKey] {
			t.Errorf("Missing KVS key in heaterKVSKeys: %s", expectedKey)
		}
	}

	// Check for extra keys in heaterKVSKeys
	for actualKey := range actualKeys {
		if !expectedKeys[actualKey] {
			t.Errorf("Extra KVS key in heaterKVSKeys (not in CONFIG_SCHEMA): %s", actualKey)
		}
	}

	// Verify count matches
	if len(heaterKVSKeys) != len(expectedKeys) {
		t.Errorf("Key count mismatch: heaterKVSKeys has %d keys, CONFIG_SCHEMA has %d keys",
			len(heaterKVSKeys), len(expectedKeys))
	}

	t.Logf("✓ Validated %d KVS keys match CONFIG_SCHEMA", len(heaterKVSKeys))
}

// schemaEntry represents a CONFIG_SCHEMA entry
type schemaEntry struct {
	Key         string
	Description string
	Default     interface{}
	Type        string
	Unprefixed  bool
}

// parseConfigSchema extracts CONFIG_SCHEMA from heater.js using goja JavaScript interpreter
// Returns a map of JavaScript field name -> schema entry
func parseConfigSchema(t *testing.T, jsCode string) map[string]schemaEntry {
	// Create a new JavaScript runtime
	vm := goja.New()

	// Execute the entire JavaScript code
	// The heater.js script now guards initialization code with typeof Shelly !== 'undefined'
	// so it won't execute when Shelly is not defined
	_, err := vm.RunString(jsCode)
	if err != nil {
		t.Fatalf("Failed to execute JavaScript: %v", err)
	}

	// Get CONFIG_SCHEMA from the runtime
	configSchemaValue := vm.Get("CONFIG_SCHEMA")
	if configSchemaValue == nil || goja.IsUndefined(configSchemaValue) {
		t.Fatal("CONFIG_SCHEMA is not defined in heater.js")
	}

	// Convert to a Go map
	configSchemaObj := configSchemaValue.ToObject(vm)
	if configSchemaObj == nil {
		t.Fatal("CONFIG_SCHEMA is not an object")
	}

	schema := make(map[string]schemaEntry)

	// Iterate over all keys in CONFIG_SCHEMA
	for _, fieldName := range configSchemaObj.Keys() {
		fieldValue := configSchemaObj.Get(fieldName)
		if fieldValue == nil || goja.IsUndefined(fieldValue) {
			continue
		}

		fieldObj := fieldValue.ToObject(vm)
		if fieldObj == nil {
			continue
		}

		// Extract properties from the field object
		entry := schemaEntry{}

		// Get key
		if keyVal := fieldObj.Get("key"); keyVal != nil && !goja.IsUndefined(keyVal) {
			entry.Key = keyVal.String()
		} else {
			t.Logf("Warning: field %s has no 'key' property", fieldName)
			continue
		}

		// Get description
		if descVal := fieldObj.Get("description"); descVal != nil && !goja.IsUndefined(descVal) {
			entry.Description = descVal.String()
		}

		// Get type
		if typeVal := fieldObj.Get("type"); typeVal != nil && !goja.IsUndefined(typeVal) {
			entry.Type = typeVal.String()
		}

		// Get default value
		if defaultVal := fieldObj.Get("default"); defaultVal != nil && !goja.IsUndefined(defaultVal) {
			entry.Default = defaultVal.Export()
		}

		// Get unprefixed flag
		if unprefixedVal := fieldObj.Get("unprefixed"); unprefixedVal != nil && !goja.IsUndefined(unprefixedVal) {
			if b, ok := unprefixedVal.Export().(bool); ok {
				entry.Unprefixed = b
			}
		}

		schema[fieldName] = entry

		t.Logf("Parsed schema field: %s (key=%s, type=%s, unprefixed=%v, default=%v)",
			fieldName, entry.Key, entry.Type, entry.Unprefixed, entry.Default)
	}

	if len(schema) == 0 {
		t.Fatal("CONFIG_SCHEMA is empty or could not be parsed")
	}

	return schema
}
