module github.com/asnowfix/home-automation

go 1.23.0

require (
	devices/shelly/gen1 v0.0.0-00010101000000-000000000000
	github.com/go-logr/logr v1.4.2
	github.com/spf13/cobra v1.8.0
	internal/myip v1.0.0
	internal/myzone v0.0.0-00010101000000-000000000000
)

replace internal/myip => ./internal/myip

replace internal/myzone => ./internal/myzone

replace mymqtt => ./mymqtt

replace devices => ./devices

replace internal => ./internal

replace devices/shelly => ./devices/shelly

replace devices/shelly/gen1 => ./devices/shelly/gen1

replace devices/shelly/types => ./devices/shelly/types

replace devices/shelly/sswitch => ./devices/shelly/sswitch

replace devices/shelly/shttp => ./devices/shelly/shttp

require (
	cloud.google.com/go/compute v1.23.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	devices/shelly v0.0.0-00010101000000-000000000000 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/eclipse/paho.mqtt.golang v1.5.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/s2a-go v0.1.7 // indirect
	github.com/google/uuid v1.3.1 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.1 // indirect
	github.com/googleapis/gax-go/v2 v2.12.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/miekg/dns v1.1.27 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.25.0 // indirect
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/oauth2 v0.13.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/api v0.148.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231012201019-e917dd12ba7a // indirect
	google.golang.org/grpc v1.58.3 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	mymqtt v0.0.0-00010101000000-000000000000 // indirect
)
