module github.com/asnowfix/home-automation/internal/myhome/ui

go 1.25.0

require (
	github.com/asnowfix/home-automation/internal/myhome v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/events v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/storage v0.0.0-00010101000000-000000000000
	github.com/dop251/goja v0.0.0-20251103141225-af2ceb9156d7
	github.com/go-logr/logr v1.4.3
)

require (
	github.com/asnowfix/home-automation/myhome/devices v0.0.0-00010101000000-000000000000
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20250317173921-a4b03ec1a45e // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/jmoiron/sqlx v1.4.0 // indirect
	github.com/miekg/dns v1.1.65 // indirect
	github.com/ncruces/go-sqlite3 v0.22.0 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	golang.org/x/mod v0.26.0 // indirect
	golang.org/x/net v0.42.0
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
)

replace github.com/asnowfix/home-automation/myhome/events => ../../../myhome/events

replace github.com/asnowfix/home-automation/myhome/storage => ../../../myhome/storage

replace github.com/asnowfix/home-automation/internal/myhome => ../

replace github.com/asnowfix/home-automation/myhome/devices => ../../../myhome/devices
