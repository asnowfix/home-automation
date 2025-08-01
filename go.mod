module github.com/asnowfix/home-automation

go 1.23.0

require (
	github.com/spf13/cobra v1.8.1
	hlog v0.0.0-00010101000000-000000000000
	homectl/options v0.0.0-00010101000000-000000000000
	internal/global v0.0.0-00010101000000-000000000000
	internal/myhome v0.0.0-00010101000000-000000000000
	internal/myip v0.0.0-00010101000000-000000000000
	internal/myzone v0.0.0-00010101000000-000000000000
	mymqtt v0.0.0-00010101000000-000000000000
	pkg/shelly v0.0.0-00010101000000-000000000000
	pkg/shelly/types v0.0.0-00010101000000-000000000000
)

replace hlog => ./hlog

replace homectl/options => ./homectl/options

replace internal/global => ./internal/global

replace internal/myhome => ./internal/myhome

replace internal/myip => ./internal/myip

replace internal/myzone => ./internal/myzone

replace devices => ./devices

replace mymqtt => ./mymqtt

replace internal => ./internal

replace pkg/shelly => ./pkg/shelly

replace pkg/shelly/gen1 => ./pkg/shelly/gen1

replace pkg/shelly/types => ./pkg/shelly/types

replace pkg/shelly/sswitch => ./pkg/shelly/sswitch

replace pkg/shelly/shttp => ./pkg/shelly/shttp

require (
	cloud.google.com/go/compute v1.24.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/eclipse/paho.mqtt.golang v1.5.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/s2a-go v0.1.7 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.2 // indirect
	github.com/googleapis/gax-go/v2 v2.12.3 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/miekg/dns v1.1.65 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.49.0 // indirect
	go.opentelemetry.io/otel v1.24.0 // indirect
	go.opentelemetry.io/otel/metric v1.24.0 // indirect
	go.opentelemetry.io/otel/trace v1.24.0 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/mod v0.24.0 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/oauth2 v0.18.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	golang.org/x/text v0.24.0 // indirect
	golang.org/x/tools v0.32.0 // indirect
	google.golang.org/api v0.171.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240314234333-6e1732d8331c // indirect
	google.golang.org/grpc v1.62.1 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	pkg/shelly/shttp v0.0.0-00010101000000-000000000000 // indirect
	pkg/shelly/sswitch v0.0.0-00010101000000-000000000000 // indirect
)
