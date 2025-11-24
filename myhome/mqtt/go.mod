module myhome/mqtt

go 1.24.2

toolchain go1.24.3

require (
	github.com/eclipse/paho.mqtt.golang v1.5.0
	github.com/go-logr/logr v1.4.3
	github.com/mochi-mqtt/server/v2 v2.6.6
	global v0.0.0-00010101000000-000000000000
)

require (
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgraph-io/ristretto v0.2.0
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/rs/xid v1.5.0 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace global => ../../internal/global
