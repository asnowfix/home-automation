// Package main implements tools/genconfigschema: a generator that reads a
// single JSON schema file per Shelly script (e.g. pool-pump.schema.json) and
// produces both the JS CONFIG_SCHEMA (and, for garden.js, ZONE_KEY_SPECS)
// block(s) injected in place into the .js source, and the Go constants/KVS
// key maps that used to be hand-maintained or regex-scraped from the JS.
//
// See GitHub issue #439 for the design rationale (Option A: JSON is the
// single source of truth; the JS schema block is generated from it, with
// `description` emitted as `//` comments so it never reaches the device).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// defaultMaxKVSKeyLen is the Shelly KVS key length ceiling. Keys must stay
// strictly below this; see the "// NN chars ✓" comments this generator
// replaces in PoolKVSKeys (internal/myhome/shelly/script/pool.go, pre-#439).
const defaultMaxKVSKeyLen = 42

// Field describes one CONFIG_SCHEMA entry: a single named, KVS-backed,
// typed configuration value with a default.
type Field struct {
	// Name is the JS/schema property name (camelCase), e.g. "ecoSpeed".
	Name string `json:"name"`
	// Description becomes a `//` comment above the field in the generated
	// JS block — never an object property, so it costs no device heap.
	Description string `json:"description"`
	// Key is the KVS key suffix (kebab-case), appended to the script's
	// KVS prefix, e.g. "eco-speed" -> "script/pool-pump/eco-speed".
	Key string `json:"key"`
	// Default is the JSON-typed default value (bool/number/string/null).
	Default any `json:"default"`
	// Type is the JS runtime type tag used by loadConfig's KVS-value
	// coercion: "boolean" | "number" | "string".
	Type string `json:"type"`
	// CliOnly marks fields written by the CLI but never read at runtime
	// (skipped by the device's KVS-load loop).
	CliOnly bool `json:"cliOnly,omitempty"`
	// Required marks fields that must resolve to a non-null value (KVS or
	// default) or the script refuses to start.
	Required bool `json:"required,omitempty"`
	// GoConst, when true, emits a `DefaultXxx` Go constant for this field.
	// Not every field has one today (e.g. preferredDeviceId has no default
	// worth naming), so this defaults to false to match current behaviour.
	GoConst bool `json:"goConst,omitempty"`
	// GoConstName overrides the constant name (default: capitalized Name).
	// Used e.g. for "nightRunDurationMs" -> "DefaultNightRunDuration".
	GoConstName string `json:"goConstName,omitempty"`
	// GoConstType overrides the emitted constant's Go type: "" (untyped
	// literal), "float64", or "time.Duration" (value is multiplied by
	// time.Millisecond; only meaningful when Type is "number").
	GoConstType string `json:"goConstType,omitempty"`
}

// ZoneField describes one entry of garden.js's ZONE_KEY_SPECS: a mapping
// from a per-zone struct field to its KVS key suffix. Unlike Field, it has
// no single default (defaults live per zone instance, e.g. ZONE_DEFAULTS).
type ZoneField struct {
	Field       string `json:"field"`
	Key         string `json:"key"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// Schema is the single source of truth for one script's configuration:
// parsed directly by Go (encoding/json) and used to generate both the JS
// CONFIG_SCHEMA block and the Go KVS-key maps/Default constants.
type Schema struct {
	// Script is the target .js file name, e.g. "pool-pump.js".
	Script string `json:"script"`
	// KVSPrefix is prepended to every field's Key, e.g. "script/pool-pump/".
	KVSPrefix string `json:"kvsPrefix"`
	// KVSKeysVar is the generated Go map variable name, e.g. "PoolKVSKeys".
	KVSKeysVar string `json:"kvsKeysVar"`
	// MaxKVSKeyLen overrides the default 42-char Shelly KVS key ceiling.
	MaxKVSKeyLen int `json:"maxKvsKeyLen,omitempty"`
	// Fields are the CONFIG_SCHEMA entries, in declaration order.
	Fields []Field `json:"fields"`
	// ZoneFields, if present, are garden.js's ZONE_KEY_SPECS entries.
	ZoneFields []ZoneField `json:"zoneFields,omitempty"`
	// ZoneFieldKeysVar is the generated Go map variable name for
	// ZoneFields, e.g. "ZoneFieldKeys".
	ZoneFieldKeysVar string `json:"zoneFieldKeysVar,omitempty"`
}

// LoadSchema reads and validates a schema JSON file.
func LoadSchema(path string) (*Schema, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading schema %s: %w", path, err)
	}
	var s Schema
	if err := json.Unmarshal(buf, &s); err != nil {
		return nil, fmt.Errorf("parsing schema %s: %w", path, err)
	}
	if s.MaxKVSKeyLen == 0 {
		s.MaxKVSKeyLen = defaultMaxKVSKeyLen
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("validating schema %s: %w", path, err)
	}
	return &s, nil
}

// Validate checks structural invariants and the Shelly KVS key length
// budget. It fails generation rather than relying on a human counting
// characters in a comment (see #439).
func (s *Schema) Validate() error {
	if s.Script == "" {
		return fmt.Errorf("schema: \"script\" is required")
	}
	if s.KVSPrefix == "" {
		return fmt.Errorf("schema: \"kvsPrefix\" is required")
	}
	if s.KVSKeysVar == "" {
		return fmt.Errorf("schema: \"kvsKeysVar\" is required")
	}
	seen := map[string]bool{}
	for _, f := range s.Fields {
		if f.Name == "" || f.Key == "" || f.Type == "" {
			return fmt.Errorf("field %+v: name, key and type are all required", f)
		}
		if seen[f.Name] {
			return fmt.Errorf("field %q: duplicate name", f.Name)
		}
		seen[f.Name] = true
		switch f.Type {
		case "boolean", "number", "string":
		default:
			return fmt.Errorf("field %q: unknown type %q (want boolean|number|string)", f.Name, f.Type)
		}
		total := len(s.KVSPrefix) + len(f.Key)
		if total >= s.MaxKVSKeyLen {
			return fmt.Errorf("field %q: KVS key %q%q is %d chars, must be < %d (Shelly KVS key limit)",
				f.Name, s.KVSPrefix, f.Key, total, s.MaxKVSKeyLen)
		}
	}
	if len(s.ZoneFields) > 0 && s.ZoneFieldKeysVar == "" {
		return fmt.Errorf("schema: \"zoneFieldKeysVar\" is required when \"zoneFields\" is set")
	}
	seenZone := map[string]bool{}
	for _, zf := range s.ZoneFields {
		if zf.Field == "" || zf.Key == "" || zf.Type == "" {
			return fmt.Errorf("zoneField %+v: field, key and type are all required", zf)
		}
		if seenZone[zf.Field] {
			return fmt.Errorf("zoneField %q: duplicate field", zf.Field)
		}
		seenZone[zf.Field] = true
		// Zone keys are namespaced "zoneN-<suffix>"; validate against the
		// worst realistic case (single-digit zone index, "zoneN-" = 6
		// chars) same as the pre-#439 hand test
		// (garden/setup_test.go TestDefaultZoneKVS_KeyLengthBudget).
		const zoneInfix = "zoneN-"
		total := len(s.KVSPrefix) + len(zoneInfix) + len(zf.Key)
		if total >= s.MaxKVSKeyLen {
			return fmt.Errorf("zoneField %q: KVS key %q%q%q is %d chars, must be < %d",
				zf.Field, s.KVSPrefix, zoneInfix, zf.Key, total, s.MaxKVSKeyLen)
		}
	}
	return nil
}

// camelBoundary matches the point right before an uppercase letter that
// follows a lowercase letter or digit, e.g. the "rS" in "solarStart".
var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// camelToSnake converts a camelCase JS field name to the snake_case Go map
// key used by the hand-maintained PoolKVSKeys/gardenKVSKeys maps this
// generator replaces, e.g. "solarStartThresholdW" -> "solar_start_threshold_w".
func camelToSnake(s string) string {
	return strings.ToLower(camelBoundary.ReplaceAllString(s, "${1}_${2}"))
}

// capitalize upper-cases the first rune, e.g. "ecoSpeed" -> "EcoSpeed". Used
// as the default Go constant name for a field unless GoConstName overrides it.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// GoConstName returns the effective Go constant name for f (DefaultXxx,
// without the "Default" prefix — callers add it).
func (f Field) GoConstNameOrDefault() string {
	if f.GoConstName != "" {
		return f.GoConstName
	}
	return capitalize(f.Name)
}

// GoName returns the snake_case Go map key for f.
func (f Field) GoName() string {
	return camelToSnake(f.Name)
}

// KVSKey returns the full KVS key (prefix + suffix) for f.
func (f Field) KVSKey(prefix string) string {
	return prefix + f.Key
}
