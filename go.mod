module github.com/asnowfix/home-automation

go 1.21.3

require (
	devices/shelly v0.0.0-00010101000000-000000000000
	devices/shelly/sswitch v0.0.0-00010101000000-000000000000
	github.com/mochi-mqtt/server/v2 v2.4.1
	internal/myip v1.0.0
	internal/myzone v0.0.0-00010101000000-000000000000
)

replace internal/myip => ./internal/myip

replace internal/myzone => ./internal/myzone

replace devices => ./devices

replace internal => ./internal

replace devices/shelly => ./devices/shelly

replace devices/shelly/types => ./devices/shelly/types

replace devices/shelly/sswitch => ./devices/shelly/sswitch

require (
	cloud.google.com/go/compute v1.23.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	github.com/alexflint/go-arg v1.4.3 // indirect
	github.com/alexflint/go-scalar v1.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/s2a-go v0.1.7 // indirect
	github.com/google/uuid v1.3.1 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.1 // indirect
	github.com/googleapis/gax-go/v2 v2.12.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/hashicorp/mdns v1.0.5 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/j-iot/tapo-go v0.0.0-20210626000425-49dce7306511 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/mergermarket/go-pkcs7 v0.0.0-20170926155232-153b18ea13c9 // indirect
	github.com/miekg/dns v1.1.41 // indirect
	github.com/rs/xid v1.5.0 // indirect
	github.com/spf13/cobra v1.8.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/oauth2 v0.13.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	google.golang.org/api v0.148.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231012201019-e917dd12ba7a // indirect
	google.golang.org/grpc v1.58.3 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
