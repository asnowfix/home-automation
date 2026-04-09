module github.com/asnowfix/home-automation/internal/myhome/ui

go 1.24.2

require (
	github.com/go-logr/logr v1.4.3
	github.com/asnowfix/home-automation/internal/myhome v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/myhome/storage v0.0.0-00010101000000-000000000000
)

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/jmoiron/sqlx v1.4.0 // indirect
	github.com/miekg/dns v1.1.65 // indirect
	github.com/ncruces/go-sqlite3 v0.22.0 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	golang.org/x/mod v0.26.0 // indirect
	golang.org/x/net v0.42.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
)

replace github.com/asnowfix/home-automation/myhome/storage => ../../../myhome/storage

replace github.com/asnowfix/home-automation/internal/myhome => ../
