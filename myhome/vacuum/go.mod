module github.com/asnowfix/home-automation/myhome/vacuum

go 1.25.0

require (
	github.com/asnowfix/home-automation/internal/myhome v0.0.0-00010101000000-000000000000
	github.com/asnowfix/home-automation/pkg/roomba v0.0.0-00010101000000-000000000000
	github.com/go-logr/logr v1.4.3
	github.com/jmoiron/sqlx v1.4.0
	modernc.org/sqlite v1.50.0
)

replace github.com/asnowfix/home-automation/internal/myhome => ../../internal/myhome

replace github.com/asnowfix/home-automation/pkg/roomba => ../../pkg/roomba
