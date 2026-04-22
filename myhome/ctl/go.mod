module github.com/asnowfix/home-automation/myhome/ctl

go 1.24.2

toolchain go1.24.3

require (
	github.com/asnowfix/home-automation/internal/debug v0.0.0-00010101000000-000000000000
	github.com/spf13/cobra v1.10.1
	github.com/asnowfix/home-automation/internal/global v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/hlog v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/internal/myhome v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/blu v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/forget v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/list v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/mqtt v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/open v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/options v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/sfr v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/shelly v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/show v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/sswitch v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/ctl/temperature v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/mqtt v0.0.0-00010101000000-000000000000
	github.com/asnowfix/go-shellies v0.0.0
	github.com/asnowfix/go-shellies/script v0.0.0
)

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/eclipse/paho.mqtt.golang v1.5.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/zerologr v1.2.3 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kardianos/service v1.2.4 // indirect
	github.com/mark3labs/mcp-go v0.32.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/miekg/dns v1.1.65 // indirect
	github.com/mochi-mqtt/server/v2 v2.6.6 // indirect
	github.com/rs/xid v1.5.0 // indirect
	github.com/rs/zerolog v1.33.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	golang.org/x/mod v0.26.0 // indirect
	golang.org/x/net v0.42.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/asnowfix/home-automation/myhome/ctl/blu => ./blu

replace github.com/asnowfix/home-automation/myhome/ctl/forget => ./forget

replace github.com/asnowfix/home-automation/myhome/ctl/list => ./list

replace github.com/asnowfix/home-automation/myhome/ctl/mqtt => ./mqtt

replace github.com/asnowfix/home-automation/myhome/ctl/open => ./open

replace github.com/asnowfix/home-automation/myhome/ctl/options => ./options

replace github.com/asnowfix/home-automation/myhome/ctl/sfr => ./sfr

replace github.com/asnowfix/home-automation/myhome/ctl/shelly => ./shelly

replace github.com/asnowfix/home-automation/myhome/ctl/show => ./show

replace github.com/asnowfix/home-automation/myhome/ctl/sswitch => ./sswitch

replace github.com/asnowfix/home-automation/myhome/ctl/temperature => ./temperature

replace github.com/asnowfix/home-automation/internal/global => ../../internal/global

replace github.com/asnowfix/home-automation/hlog => ../../hlog

replace github.com/asnowfix/home-automation/internal/myhome => ../../internal/myhome

replace github.com/asnowfix/home-automation/myhome/mqtt => ../mqtt


replace github.com/asnowfix/home-automation/internal/debug => ../../internal/debug
