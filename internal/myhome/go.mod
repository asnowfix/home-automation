module myhome

go 1.24.2

toolchain go1.24.3

require (
	github.com/go-logr/logr v1.4.3
	github.com/grandcat/zeroconf v1.0.0
	myhome/net v0.0.0-00010101000000-000000000000
)

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/jackpal/gateway v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/miekg/dns v1.1.65 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/mod v0.26.0 // indirect
	golang.org/x/net v0.42.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace myhome/net => ./net
