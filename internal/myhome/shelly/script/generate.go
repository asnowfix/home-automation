package script

// pool_kvs_generated.go is produced from internal/shelly/scripts/pool-pump.schema.json,
// the single source of truth also used to generate pool-pump.js's CONFIG_SCHEMA
// block and myhome/ctl/pool's Default* constants — see issue #439. PoolKVSKeys
// lives in this package (rather than myhome/ctl/pool) because pool.go's
// PoolService business logic, which consumes it, lives here too; myhome/ctl/pool
// already imports this package, so generating PoolKVSKeys there instead would
// create an import cycle.
//go:generate go run ../../../../tools/genconfigschema -schema ../../../shelly/scripts/pool-pump.schema.json -go pool_kvs_generated.go -go-package script -kvskeys
