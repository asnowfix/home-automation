module github.com/asnowfix/home-automation

go 1.25.0

require (
	github.com/asnowfix/home-automation/hlog v0.0.0-20260402201030-0ed25e95389f
	github.com/asnowfix/home-automation/internal/myhome v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/internal/myip v0.0.0-20260402201030-0ed25e95389f
	github.com/asnowfix/home-automation/internal/myzone v0.0.0-20260402201030-0ed25e95389f
	github.com/asnowfix/home-automation/myhome/ctl/options v0.0.0-20260402201030-0ed25e95389f
	github.com/asnowfix/home-automation/myhome/temperature v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/pkg/shelly v0.0.0-20260402201030-0ed25e95389f
	github.com/asnowfix/home-automation/pkg/shelly/types v0.0.0-20260402201030-0ed25e95389f
	github.com/go-logr/logr v1.4.3
	github.com/spf13/cobra v1.10.2
	github.com/spf13/viper v1.21.0
)

replace github.com/asnowfix/home-automation/hlog => ./hlog

replace github.com/asnowfix/home-automation/myhome/ctl/options => ./myhome/ctl/options

replace github.com/asnowfix/home-automation/myhome/temperature => ./myhome/temperature

replace github.com/asnowfix/home-automation/internal/global => ./internal/global

replace github.com/asnowfix/home-automation/internal/myhome => ./internal/myhome

replace github.com/asnowfix/home-automation/internal/myip => ./internal/myip

replace github.com/asnowfix/home-automation/internal/myzone => ./internal/myzone

replace github.com/asnowfix/home-automation/internal/shelly/scripts => ./internal/shelly/scripts

replace github.com/asnowfix/home-automation/pkg/shelly => ./pkg/shelly

replace github.com/asnowfix/home-automation/pkg/shelly/gen1 => ./pkg/shelly/gen1

replace github.com/asnowfix/home-automation/pkg/shelly/types => ./pkg/shelly/types

replace github.com/asnowfix/home-automation/pkg/shelly/sswitch => ./pkg/shelly/sswitch

replace github.com/asnowfix/home-automation/pkg/shelly/shttp => ./pkg/shelly/shttp

require (
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute v1.59.0 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/asnowfix/home-automation/internal/shelly/scripts v0.0.0-20260402201030-0ed25e95389f // indirect
	github.com/asnowfix/home-automation/pkg/shelly/shttp v0.0.0-20260402201030-0ed25e95389f // indirect
	github.com/asnowfix/home-automation/pkg/shelly/sswitch v0.0.0-20260402201030-0ed25e95389f // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-logr/zerologr v1.2.3 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.14 // indirect
	github.com/googleapis/gax-go/v2 v2.21.0 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kardianos/service v1.2.4 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.21 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/pelletier/go-toml/v2 v2.3.0 // indirect
	github.com/rs/zerolog v1.35.0 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/mod v0.34.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	golang.org/x/tools v0.43.0 // indirect
	google.golang.org/api v0.275.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
