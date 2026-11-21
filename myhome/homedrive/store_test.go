package homedrive

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// memDB is an in-memory SQLite database; one connection, because each
// connection to ":memory:" would otherwise get its own empty database.
func memDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStore_RoundTrip(t *testing.T) {
	db := memDB(t)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(db); err != nil {
		t.Fatalf("NewStore must be idempotent: %v", err)
	}
	if err := s.Save(Row{Host: "h1", User: "u", LastTs: 100, LastStatus: `{"a":1}`}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Row{Host: "h2", User: "u", LastTs: 200, LastStatus: `{"b":2}`}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Row{Host: "h1", User: "u", LastTs: 300, LastStatus: `{"a":3}`}); err != nil { // upsert
		t.Fatal(err)
	}
	rows, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Host != "h1" || rows[0].LastTs != 300 || rows[0].LastStatus != `{"a":3}` || rows[1].Host != "h2" {
		t.Fatalf("rows = %+v", rows)
	}
	if err := s.Delete("h1", "u"); err != nil {
		t.Fatal(err)
	}
	if rows, _ = s.Load(); len(rows) != 1 || rows[0].Host != "h2" {
		t.Fatalf("after delete: %+v", rows)
	}
}

func TestStore_ClosedDBErrors(t *testing.T) {
	db := memDB(t)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := s.Save(Row{Host: "h", User: "u"}); err == nil {
		t.Error("Save on a closed db must fail")
	}
	if _, err := s.Load(); err == nil {
		t.Error("Load on a closed db must fail")
	}
	if err := s.Delete("h", "u"); err == nil {
		t.Error("Delete on a closed db must fail")
	}
	if _, err := NewStore(db); err == nil {
		t.Error("NewStore on a closed db must fail")
	}
}
