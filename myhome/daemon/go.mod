module github.com/asnowfix/home-automation/myhome/daemon

go 1.25.0

toolchain go1.25.3

require (
	github.com/asnowfix/home-automation/myhome/mqtt v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/pkg/beem v0.0.0-00010101000000-000000000000
	github.com/go-logr/logr v1.4.3
	github.com/kardianos/service v1.2.4
	github.com/spf13/cobra v1.10.1
	modernc.org/sqlite v1.50.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sys v0.34.0 // indirect
)

replace github.com/asnowfix/home-automation/myhome/mqtt => ../mqtt
replace github.com/asnowfix/home-automation/pkg/beem => ../../pkg/beem
