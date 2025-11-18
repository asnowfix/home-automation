module myhome/temperature

go 1.24.2

require (
	github.com/go-logr/logr v1.4.3
	github.com/jmoiron/sqlx v1.4.0
	github.com/ncruces/go-sqlite3 v0.22.0
	github.com/spf13/viper v1.21.0
	myhome v0.0.0-00010101000000-000000000000
	myhome/mqtt v0.0.0-00010101000000-000000000000
)

replace myhome => ../../internal/myhome

replace myhome/mqtt => ../mqtt

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgraph-io/ristretto v0.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/eclipse/paho.mqtt.golang v1.5.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/miekg/dns v1.1.65 // indirect
	github.com/mochi-mqtt/server/v2 v2.6.6 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rs/xid v1.5.0 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.26.0 // indirect
	golang.org/x/net v0.42.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
