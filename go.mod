module github.com/asnowfix/home-automation

go 1.25.0

toolchain go1.25.3

require (
	github.com/asnowfix/home-automation/pkg/beem v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/pkg/sfr v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/pkg/shelly v0.0.0-00010101000000-000000000000
	github.com/dgraph-io/ristretto v0.2.0
	github.com/dop251/goja v0.0.0-20251103141225-af2ceb9156d7
	github.com/eclipse/paho.mqtt.golang v1.5.1
	github.com/go-logr/logr v1.4.3
	github.com/go-logr/zerologr v1.2.3
	github.com/grandcat/zeroconf v1.0.0
	github.com/j-iot/tapo-go v0.0.0-20210626000425-49dce7306511
	github.com/jackpal/gateway v1.1.1
	github.com/jmoiron/sqlx v1.4.0
	github.com/kardianos/service v1.2.4
	github.com/mark3labs/mcp-go v0.32.0
	github.com/mattn/go-isatty v0.0.21
	github.com/mochi-mqtt/server/v2 v2.6.6
	github.com/pion/mdns/v2 v2.0.7
	github.com/rs/zerolog v1.35.0
	github.com/spf13/cobra v1.10.2
	github.com/spf13/viper v1.21.0
	go.yaml.in/yaml/v3 v3.0.4
	golang.org/x/net v0.55.0
	golang.org/x/sys v0.45.0
	google.golang.org/api v0.275.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.50.0
	sigs.k8s.io/yaml v1.6.0
)

require (
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/google/pprof v0.0.0-20250317173921-a4b03ec1a45e // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.14 // indirect
	github.com/googleapis/gax-go/v2 v2.21.0 // indirect
	github.com/gorilla/schema v1.4.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mergermarket/go-pkcs7 v0.0.0-20170926155232-153b18ea13c9 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pelletier/go-toml/v2 v2.3.0 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tdewolff/minify/v2 v2.24.3 // indirect
	github.com/tdewolff/parse/v2 v2.8.3 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/exp v0.0.0-20250106191152-7588d65b2ba8 // indirect
	golang.org/x/mod v0.35.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	golang.org/x/tools v0.44.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	modernc.org/libc v1.72.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

// pkg/shelly, pkg/sfr and pkg/beem are standalone library modules within this
// repo (see docs/EXTRACT-PKG-SHELLY-PLAN.md for pkg/shelly's longer-term
// extraction to its own repository). Until they are tagged as independent
// releases, the root module depends on them via local replace directives —
// these three are the only ones that should remain in this file (#359).
replace github.com/asnowfix/home-automation/pkg/shelly => ./pkg/shelly

replace github.com/asnowfix/home-automation/pkg/sfr => ./pkg/sfr

replace github.com/asnowfix/home-automation/pkg/beem => ./pkg/beem
