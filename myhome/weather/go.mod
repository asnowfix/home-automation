module github.com/asnowfix/home-automation/myhome/weather

go 1.25.0

require (
	github.com/asnowfix/home-automation/myhome/mqtt v0.0.0-00010101000000-000000000000
	github.com/go-logr/logr v1.4.3
	github.com/jmoiron/sqlx v1.4.0
	modernc.org/sqlite v1.50.0
)

replace github.com/asnowfix/home-automation/myhome/mqtt => ../mqtt
