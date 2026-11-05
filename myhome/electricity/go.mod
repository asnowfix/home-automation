module github.com/asnowfix/home-automation/myhome/electricity

go 1.25.0

require (
	github.com/asnowfix/home-automation/myhome/mqtt v0.0.0-00010101000000-000000000000
	github.com/go-logr/logr v1.4.3
)

replace github.com/asnowfix/home-automation/myhome/mqtt => ../mqtt
