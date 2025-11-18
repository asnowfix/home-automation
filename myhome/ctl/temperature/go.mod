module myhome/ctl/temperature

go 1.24.2

require (
	github.com/go-logr/logr v1.4.3
	github.com/spf13/cobra v1.10.1
	myhome v0.0.0-00010101000000-000000000000
	myhome/ctl/options v0.0.0-00010101000000-000000000000
	myhome/mqtt v0.0.0-00010101000000-000000000000
)

replace myhome => ../../../internal/myhome

replace myhome/ctl/options => ../options

replace myhome/mqtt => ../../mqtt
