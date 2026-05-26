module github.com/asnowfix/home-automation/myhome/devices/impl

go 1.25.0

toolchain go1.25.3

require (
	github.com/asnowfix/home-automation/myhome/events v0.0.0-00010101000000-000000000000
	github.com/go-logr/logr v1.4.3
)

replace github.com/asnowfix/home-automation/myhome/events => ../../events
