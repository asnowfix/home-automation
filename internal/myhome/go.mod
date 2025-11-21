module myhome

go 1.24.2

toolchain go1.24.3

require (
	github.com/go-logr/logr v1.4.3
	github.com/grandcat/zeroconf v1.0.0
	mynet v0.0.0-00010101000000-000000000000
)

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/miekg/dns v1.1.65 // indirect
	golang.org/x/mod v0.24.0 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/tools v0.32.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace mynet => ../mynet
