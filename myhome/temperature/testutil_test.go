package temperature

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	"myhome/mqtt"
)

// newTestDB opens an in-memory SQLite database and registers t.Cleanup to close it.
// The SQLite driver is registered by storage.go's blank import.
func newTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("newTestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newTestStorage creates a Storage backed by an in-memory SQLite database.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := NewStorage(logr.Discard(), newTestDB(t))
	if err != nil {
		t.Fatalf("newTestStorage: %v", err)
	}
	return s
}

// newTestService creates a Service wired to an in-memory database and a
// RecordingMockClient. Callers can use mc to inspect published MQTT messages
// and inject incoming ones via mc.Feed.
func newTestService(t *testing.T) (*Service, *mqtt.RecordingMockClient) {
	t.Helper()
	mc := mqtt.NewRecordingMockClient()
	svc := NewService(context.Background(), logr.Discard(), mc, newTestStorage(t))
	return svc, mc
}
