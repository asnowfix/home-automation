module myhome/ctl

go 1.24.2

toolchain go1.24.3

require (
	debug v0.0.0-00010101000000-000000000000
	github.com/spf13/cobra v1.8.1
	global v0.0.0-00010101000000-000000000000
	hlog v0.0.0-00010101000000-000000000000
	myhome v0.0.0-00010101000000-000000000000
	myhome/ctl/follow v0.0.0-00010101000000-000000000000
	myhome/ctl/forget v0.0.0-00010101000000-000000000000
	myhome/ctl/group v0.0.0-00010101000000-000000000000
	myhome/ctl/list v0.0.0-00010101000000-000000000000
	myhome/ctl/mqtt v0.0.0-00010101000000-000000000000
	myhome/ctl/open v0.0.0-00010101000000-000000000000
	myhome/ctl/options v0.0.0-00010101000000-000000000000
	myhome/ctl/shelly v0.0.0-00010101000000-000000000000
	myhome/ctl/show v0.0.0-00010101000000-000000000000
	myhome/ctl/sswitch v0.0.0-00010101000000-000000000000
	mymqtt v0.0.0-00010101000000-000000000000
	pkg/shelly v0.0.0-00010101000000-000000000000
	pkg/shelly/types v0.0.0-00010101000000-000000000000
)

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/eclipse/paho.mqtt.golang v1.5.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/zerologr v1.2.3 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackpal/gateway v1.1.1 // indirect
	github.com/kardianos/service v1.2.4 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/miekg/dns v1.1.27 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/rs/zerolog v1.33.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	golang.org/x/crypto v0.36.0 // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace myhome/ctl/follow => ./follow

replace myhome/ctl/forget => ./forget

replace myhome/ctl/group => ./group

replace myhome/ctl/list => ./list

replace myhome/ctl/mqtt => ./mqtt

replace myhome/ctl/open => ./open

replace myhome/ctl/options => ./options

replace myhome/ctl/shelly => ./shelly

replace myhome/ctl/show => ./show

replace myhome/ctl/sswitch => ./sswitch

replace global => ../../internal/global

replace hlog => ../../hlog

replace myhome => ../../internal/myhome

replace mymqtt => ../../mymqtt

replace pkg/shelly => ../../pkg/shelly

replace pkg/shelly/types => ../../pkg/shelly/types

replace debug => ../../internal/debug
