# #705 metrics.db store: plan and progress

Issue: #705 (epic #654, phase 1). Design: `docs/observability/observability-study.md` section 6.

## Plan

1. [x] Package `myhome/events/metricsdb` (store, buffer, flush, purge, query) plus tests.
2. [x] Options in all four places: `options.go`, `run.go`, `docs/configuration.md`, `myhome-example.yaml`.
3. [x] Daemon wiring: open, degrade to disabled on error, run flush loop and retention.
4. [ ] Full `make test`.

## Decisions

- **Placement.** Sub-package `metricsdb` inside the existing `myhome/events` Go module. It needs the
  same dependencies (sqlx, modernc sqlite, logr), so no new go.work module and no new dependency.
  It is a sub-package, not part of `package events`, because `events` is the event log and this store
  has a different shape (values over time, no events). `myhome/metrics` is taken by the Prometheus exporter.
- **Schema.** `series(series_id INTEGER PRIMARY KEY, device_id, metric, UNIQUE(device_id, metric))`;
  `samples(ts INTEGER, series_id INTEGER, value REAL, PRIMARY KEY (ts, series_id)) WITHOUT ROWID`,
  no secondary index (a test asserts it). `ts` is integer unix seconds. Same-second duplicate: replaced.
- **Writes.** `Add` only buffers in memory; `Flush` (every minute from `Start`) writes the buffer in
  one transaction. A failed flush puts the samples back; the buffer is capped at 100k samples (oldest dropped, logged).
- **Retention.** Deletes one hour of time per statement (`DELETE ... WHERE ts < upper`), looping until
  the cutoff, so the write lock is never held for long. Runs at startup then hourly. 0 disables.
- **Read API.** `Query(ctx, device, metric, from, to, step)`: `[from, to)`, raw when step < 1s, else
  AVG per bucket of `step` with the bucket start as timestamp. Unknown series: no points.
- **Config.** Section `metrics_store` (not `metrics`, to avoid confusion with the Prometheus exporter
  options): `metrics_store.db` (default `metrics.db`), `metrics_store.retention` (default 168h);
  flags `--metrics-db`, `--metrics-retention`; env `MYHOME_METRICS_STORE_DB` / `_RETENTION`.
  No `enabled` switch: not asked for; a failure to open disables it.
- **Failure isolation.** An open error is logged and the store is simply absent. All methods are nil-safe.
  Wired inside the device-manager block next to the events store (the future collector needs it).

## Progress log

- Wrote store and tests; `go test -race ./metricsdb/` (in `myhome/events`) passes. A first test run
  failed because my test base time was not hour-aligned (3 buckets, not 2): a test bug, not a store bug.
- Config and wiring done; `go build ./...` passes after `make generate`.
