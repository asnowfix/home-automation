package pool

// pool_defaults_generated.go is produced from the JSON schema that is also
// the source of truth for pool-pump.js's CONFIG_SCHEMA and for PoolKVSKeys
// (internal/myhome/shelly/script/pool_kvs_generated.go) — see issue #439.
// Regenerating also rewrites pool-pump.js's CONFIG_SCHEMA block in place.
//go:generate go run ../../../tools/genconfigschema -schema ../../../internal/shelly/scripts/pool-pump.schema.json -js ../../../internal/shelly/scripts/pool-pump.js -go pool_defaults_generated.go -go-package pool -consts
