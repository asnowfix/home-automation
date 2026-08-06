module github.com/asnowfix/home-automation/pkg/beem

go 1.25.0

require (
	github.com/asnowfix/home-automation/hlog v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/mqtt v0.0.0-00010101000000-000000000000
	github.com/go-logr/logr v1.4.3
)

require (
	github.com/asnowfix/home-automation/internal/global v0.0.0-00010101000000-000000000000 // indirect
	github.com/asnowfix/home-automation/internal/myhome/net v0.0.0-20260713141241-6bc3b69c6509 // indirect
	github.com/asnowfix/home-automation/myhome/ctl/options v0.0.0-20260713141241-6bc3b69c6509 // indirect
	github.com/asnowfix/home-automation/pkg/shelly/types v0.0.0-20260713141241-6bc3b69c6509 // indirect
	github.com/asnowfix/home-automation/pkg/version v0.0.0-20260713141241-6bc3b69c6509 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgraph-io/ristretto v0.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/eclipse/paho.mqtt.golang v1.5.1 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/zerologr v1.2.3 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackpal/gateway v1.1.1 // indirect
	github.com/kardianos/service v1.2.4 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/miekg/dns v1.1.65 // indirect
	github.com/mochi-mqtt/server/v2 v2.6.6 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rs/xid v1.5.0 // indirect
	github.com/rs/zerolog v1.33.0 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.10.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/spf13/viper v1.21.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.35.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	golang.org/x/tools v0.44.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace github.com/asnowfix/home-automation/hlog => ../../hlog

replace github.com/asnowfix/home-automation/myhome/mqtt => ../../myhome/mqtt

replace github.com/asnowfix/home-automation/internal/global => ../../internal/global

replace github.com/asnowfix/home-automation/internal/myhome/net => ../../internal/myhome/net

replace github.com/asnowfix/home-automation/myhome/ctl/options => ../../myhome/ctl/options

replace github.com/asnowfix/home-automation/pkg/version => ../../pkg/version

replace github.com/asnowfix/home-automation/pkg/shelly/types => ../../pkg/shelly/types
