package metricsdb

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(logr.Discard(), filepath.Join(t.TempDir(), "sub", "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.Get(&n, `SELECT COUNT(*) FROM `+table); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSchemaWithoutRowidNoSecondaryIndex(t *testing.T) {
	s := open(t)
	var sqlText string
	if err := s.db.Get(&sqlText, `SELECT sql FROM sqlite_master WHERE name='samples'`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sqlText, "WITHOUT ROWID") || !strings.Contains(sqlText, "PRIMARY KEY (ts, series_id)") {
		t.Fatalf("unexpected schema: %s", sqlText)
	}
	var idx int
	// sqlite_autoindex entries have NULL sql; any explicit index is a violation.
	if err := s.db.Get(&idx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='samples' AND sql IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if idx != 0 {
		t.Fatalf("samples must have no secondary index, found %d", idx)
	}
}

func TestNonFiniteSamplesDoNotPoisonBatch(t *testing.T) {
	s := open(t)
	now := time.Now()
	s.Add(Sample{"d", "m", now, math.NaN()})
	s.Add(Sample{"d", "m", now.Add(time.Second), math.Inf(1)})
	s.Add(Sample{"d", "m", now.Add(2 * time.Second), math.Inf(-1)})
	s.Add(Sample{"d", "m", now.Add(3 * time.Second), 42})
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("flush must not fail on non-finite values: %v", err)
	}
	if n := count(t, s, "samples"); n != 1 {
		t.Fatalf("samples = %d, want 1 (only the finite one)", n)
	}
	// Nothing left buffered to fail again on the next flush.
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.Rejected(); got != 3 {
		t.Fatalf("Rejected = %d, want 3", got)
	}
}

func TestCapDropsAreCounted(t *testing.T) {
	s := open(t)
	now := time.Now()
	for i := 0; i < maxBuffered+10; i++ {
		s.Add(Sample{"d", "m", now.Add(time.Duration(i) * time.Second), 1})
	}
	if got := s.Dropped(); got != 10 {
		t.Fatalf("Dropped after Add overflow = %d, want 10", got)
	}
	// A failed flush puts the batch back; samples added meanwhile overflow the cap.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Flush(ctx); err == nil {
		t.Fatal("expected error")
	}
	s.Add(Sample{"d", "m", now.Add(time.Hour), 1})
	if got := s.Dropped(); got != 11 {
		t.Fatalf("Dropped = %d, want 11", got)
	}
}

func TestUnalignedFromBucketsAlignToEpoch(t *testing.T) {
	s := open(t)
	base := time.Unix(1_700_000_000-1_700_000_000%3600, 0)
	for i := 0; i < 120; i++ {
		s.Add(Sample{"d", "m", base.Add(time.Duration(i) * time.Minute), float64(i)})
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	// from is 30 minutes into the first hour: the first bucket is the epoch-aligned
	// hour start (before from) and holds only the samples at or after from.
	from := base.Add(30 * time.Minute)
	ds, err := s.Query(context.Background(), "d", "m", from, base.Add(2*time.Hour), time.Hour)
	if err != nil || len(ds) != 2 {
		t.Fatalf("len=%d err=%v", len(ds), err)
	}
	if ds[0].Ts != base.Unix() || ds[0].Value != 44.5 { // mean of minutes 30..59
		t.Fatalf("first bucket = %+v, want ts=%d value=44.5", ds[0], base.Unix())
	}
	if ds[1].Ts != base.Add(time.Hour).Unix() || ds[1].Value != 89.5 {
		t.Fatalf("second bucket = %+v", ds[1])
	}
}

func TestAddBuffersUntilFlush(t *testing.T) {
	s := open(t)
	now := time.Now()
	for i := 0; i < 5; i++ {
		s.Add(Sample{"dev1", "rssi", now.Add(time.Duration(i) * time.Second), float64(-60 - i)})
	}
	s.Add(Sample{"dev2", "rssi", now, -70})
	if n := count(t, s, "samples"); n != 0 {
		t.Fatalf("Add must not write, got %d rows", n)
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "samples"); n != 6 {
		t.Fatalf("samples = %d, want 6", n)
	}
	if n := count(t, s, "series"); n != 2 {
		t.Fatalf("series = %d, want 2", n)
	}
	// A second flush with nothing buffered is a no-op; re-adding reuses series.
	s.Add(Sample{"dev1", "rssi", now.Add(time.Hour), -1})
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "series"); n != 2 {
		t.Fatalf("series = %d after reuse, want 2", n)
	}
}

func TestFlushFailureKeepsSamples(t *testing.T) {
	s := open(t)
	s.Add(Sample{"d", "m", time.Now(), 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Flush(ctx); err == nil {
		t.Fatal("expected error with cancelled context")
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "samples"); n != 1 {
		t.Fatalf("sample lost after failed flush: %d rows", n)
	}
}

func TestPurgeBatched(t *testing.T) {
	s := open(t)
	now := time.Now()
	// 30 hours of per-minute samples: purge must loop over several 1h chunks.
	for i := 0; i < 30*60; i++ {
		s.Add(Sample{"d", "m", now.Add(-time.Duration(i) * time.Minute), 1})
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	n, err := s.Purge(context.Background(), now.Add(-10*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	left := count(t, s, "samples")
	if int(n)+left != 30*60 || left < 10*60-1 || left > 10*60+1 {
		t.Fatalf("deleted %d, left %d", n, left)
	}
	if got := s.purgeStmts.Load(); got < 19 || got > 21 {
		t.Fatalf("purge used %d DELETE statements, want ~20 (one per hour)", got)
	}
	// Idempotent.
	if n, _ := s.Purge(context.Background(), now.Add(-10*time.Hour)); n != 0 {
		t.Fatalf("second purge deleted %d", n)
	}
}

func TestQueryRawAndDownsampled(t *testing.T) {
	s := open(t)
	base := time.Unix(1_700_000_000-1_700_000_000%3600, 0) // hour-aligned
	for i := 0; i < 120; i++ {                             // two hours, one sample a minute, value = minute index
		s.Add(Sample{"d", "m", base.Add(time.Duration(i) * time.Minute), float64(i)})
		s.Add(Sample{"other", "m", base.Add(time.Duration(i) * time.Minute), 999})
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	from, to := base, base.Add(2*time.Hour)

	raw, err := s.Query(ctx, "d", "m", from, to, 0)
	if err != nil || len(raw) != 120 || raw[0].Value != 0 || raw[119].Value != 119 {
		t.Fatalf("raw: len=%d err=%v", len(raw), err)
	}
	sub, _ := s.Query(ctx, "d", "m", base.Add(10*time.Minute), base.Add(20*time.Minute), 0)
	if len(sub) != 10 || sub[0].Value != 10 {
		t.Fatalf("range [from,to): got %d points", len(sub))
	}
	ds, err := s.Query(ctx, "d", "m", from, to, time.Hour)
	if err != nil || len(ds) != 2 {
		t.Fatalf("downsampled: len=%d err=%v", len(ds), err)
	}
	if ds[0].Value != 29.5 || ds[1].Value != 89.5 {
		t.Fatalf("bucket means = %v, %v", ds[0].Value, ds[1].Value)
	}
	if ds[1].Ts-ds[0].Ts != 3600 {
		t.Fatalf("bucket spacing = %d", ds[1].Ts-ds[0].Ts)
	}
	if none, err := s.Query(ctx, "nope", "m", from, to, 0); err != nil || len(none) != 0 {
		t.Fatalf("unknown series: %v %v", none, err)
	}
}

func TestNilStoreIsNoop(t *testing.T) {
	var s *Store
	s.Add(Sample{})
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pts, err := s.Query(context.Background(), "d", "m", time.Now(), time.Now(), 0); pts != nil || err != nil {
		t.Fatal("nil store query")
	}
	if n, err := s.Purge(context.Background(), time.Now()); n != 0 || err != nil {
		t.Fatal("nil store purge")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFailureReturnsError(t *testing.T) {
	// A path under a regular file cannot be created.
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(logr.Discard(), filepath.Join(f, "metrics.db")); err == nil {
		t.Fatal("expected error")
	}
}
