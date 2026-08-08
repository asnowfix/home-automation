package garden

// garden_defaults_generated.go is produced from the JSON schema that is also
// the source of truth for garden.js's CONFIG_SCHEMA/ZONE_KEY_SPECS blocks —
// see issue #439. Regenerating also rewrites garden.js's CONFIG_SCHEMA and
// ZONE_KEY_SPECS blocks in place.
//go:generate go run ../../../tools/genconfigschema -schema ../../../internal/shelly/scripts/garden.schema.json -js ../../../internal/shelly/scripts/garden.js -go garden_defaults_generated.go -go-package garden -consts -kvskeys -zonefieldkeys
