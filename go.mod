module github.com/asnowfix/home-automation

go 1.21.4

toolchain go1.21.6

require (
	devices/shelly/gen1 v0.0.0-00010101000000-000000000000
	github.com/spf13/cobra v1.8.0
	internal/myip v1.0.0
	internal/myzone v0.0.0-00010101000000-000000000000
)

replace internal/myip => ./internal/myip

replace internal/myzone => ./internal/myzone

replace devices => ./devices

replace internal => ./internal

replace devices/shelly => ./devices/shelly

replace devices/shelly/gen1 => ./devices/shelly/gen1

replace devices/shelly/types => ./devices/shelly/types

replace devices/shelly/sswitch => ./devices/shelly/sswitch

require (
	cloud.google.com/go/compute v1.23.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	devices v0.0.0-00010101000000-000000000000 // indirect
	devices/shelly v0.0.0-00010101000000-000000000000 // indirect
	devices/shelly/types v0.0.0-00010101000000-000000000000 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/eclipse/paho.mqtt.golang v1.4.3 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/s2a-go v0.1.7 // indirect
	github.com/google/uuid v1.3.1 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.1 // indirect
	github.com/googleapis/gax-go/v2 v2.12.0 // indirect
	github.com/gorilla/schema v1.2.1 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackpal/gateway v1.0.13 // indirect
	github.com/miekg/dns v1.1.58 // indirect
	github.com/mochi-mqtt/server/v2 v2.4.6 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rs/xid v1.4.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/objx v0.5.0 // indirect
	github.com/stretchr/testify v1.8.4 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.20.0 // indirect
	golang.org/x/exp v0.0.0-20240222234643-814bf88cf225 // indirect
	golang.org/x/mod v0.15.0 // indirect
	golang.org/x/net v0.21.0 // indirect
	golang.org/x/oauth2 v0.13.0 // indirect
	golang.org/x/sync v0.6.0 // indirect
	golang.org/x/sys v0.17.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/tools v0.18.0 // indirect
	google.golang.org/api v0.148.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231012201019-e917dd12ba7a // indirect
	google.golang.org/grpc v1.58.3 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
