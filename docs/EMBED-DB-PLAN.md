# Embedded Database Provider Study

**Date:** 2026-05-09  
**Branch:** feature+db-provider  
**Status:** Option A implemented ✓

### What was done
1. Added benchmark suite: `myhome/storage/db_bench_test.go`, `myhome/temperature/storage_bench_test.go`
2. Added `PRAGMA journal_mode=WAL` + `PRAGMA synchronous=NORMAL` to `NewDeviceStorage`
3. Swapped driver: `ncruces/go-sqlite3` → `modernc.org/sqlite v1.50.0` in both modules
4. Updated driver name string: `"sqlite3"` → `"sqlite"` in connection open calls
5. All existing tests pass; benchmark results recorded in §6

---

## 1. Current Setup

| Aspect | Details |
|--------|---------|
| Driver | `github.com/ncruces/go-sqlite3 v0.22.0` (pure Go via WebAssembly/wazero) |
| Query layer | `github.com/jmoiron/sqlx v1.4.0` |
| Tables | `devices`, `temperature_rooms`, `temperature_kind_schedules`, `temperature_weekday_defaults` |
| Data volume | < 1 MB total (50–200 devices + temperature config) |
| DB file | `myhome.db` (single file, no pragmas set, no WAL mode) |
| Modules | `myhome/storage/go.mod`, `myhome/temperature/go.mod` |

### How ncruces/go-sqlite3 works

ncruces bundles the SQLite C library compiled to WebAssembly (`.wasm`) and runs it inside [wazero](https://github.com/tetratelabs/wazero), a pure-Go WebAssembly runtime. Every SQL statement dispatches through the wazero interpreter, paying:

- WebAssembly interpreter dispatch on every opcode (10–30× slower than native in interpreter mode; 2–5× in JIT mode on amd64/arm64)
- wazero heap separate from Go heap → extra allocations and GC pressure
- Binary bloat: the `.wasm` blob is embedded in the binary

This is the most expensive way to use SQLite in Go. It was chosen to avoid CGo (cross-compilation, no external `.so`). That trade-off made sense for portability but the performance cost is real and measurable.

---

## 2. Actual Data Access Patterns

After reading `myhome/storage/db.go` and `myhome/temperature/storage.go`:

| Pattern | SQL used | What it really is |
|---------|----------|-------------------|
| Look up device by (manufacturer, id) | `SELECT … WHERE manufacturer=$1 AND id=$2` | Map lookup by composite key |
| Look up device by MAC | `SELECT … WHERE mac=$1` | Map lookup by secondary key |
| Look up device by name or host | `SELECT … WHERE name=$1` / `WHERE host=$1` | Map lookup |
| Match device by any field | `WHERE name LIKE … OR id LIKE … OR mac LIKE … OR host LIKE …` | Substring filter over <200 rows |
| List all devices | `SELECT * FROM devices` | Full scan of a tiny collection |
| Upsert device | `INSERT … ON CONFLICT DO UPDATE …` | Put with change detection |
| Temperature room config | point lookup / full scan on <20 rows | Same |
| Kind schedules | `SELECT … WHERE kind=? AND day_type=?` | Composite key lookup, max 10 rows |
| Weekday defaults | `SELECT * FROM temperature_weekday_defaults` | 7-row static table |

**Conclusion: zero SQL features are actually being used.** No joins, no aggregations, no subqueries, no window functions. Every query maps to a simple key lookup or a full scan of a table with < 200 rows. The SQL layer is pure overhead.

---

## 3. Options

### Option A — Switch SQLite driver: modernc.org/sqlite

`modernc.org/sqlite` is the SQLite C source machine-translated to Go. Not WebAssembly — native Go code. Same SQL API, drop-in driver swap.

**Migration effort:** Minimal — change two import lines, bump go.mod.

```
- github.com/ncruces/go-sqlite3 → github.com/cznic/sqlite (modernc.org/sqlite)
- _ "github.com/ncruces/go-sqlite3/driver"
- _ "github.com/ncruces/go-sqlite3/embed"
+ _ "modernc.org/sqlite"
```

**Performance vs ncruces:** 3–6× faster per query (rough, benchmark-dependent). Still slower than native CGo but significantly better than WASM interpretation.

**Pros:**
- Pure Go, no CGo
- Near-drop-in: same `database/sql` interface, `sqlx` continues to work
- Keeps all existing SQL logic intact
- No architectural change

**Cons:**
- Data is still this small; you're optimizing a cold path
- SQL layer overhead remains (parsing, planning, row iteration)
- Doesn't fix the "SQL is overkill" architectural mismatch
- Binary still ~5–8 MB larger than a pure KV store

**Verdict:** Best choice **if SQL must be kept** and refactor budget is zero.

---

### Option B — Replace with bbolt (go.etcd.io/bbolt)

[bbolt](https://github.com/etcd-io/bbolt) is a pure-Go B-tree key-value store. Single file, ACID, memory-mapped. Used in production by etcd, Kubernetes, Gitea, and many others. Forked from original BoltDB; actively maintained.

**API model:** Buckets (named namespaces) → byte key → byte value. Reads are lock-free (MVCC). Writes take a file lock.

**Mapping:**

| Current SQL table | bbolt bucket | Key | Value |
|---|---|---|---|
| `devices` (PK) | `devices` | `manufacturer\x00id` | JSON-encoded Device |
| `devices` (by MAC) | `devices_by_mac` | mac | `manufacturer\x00id` (index) |
| `temperature_rooms` | `temp_rooms` | room_id | JSON-encoded Room |
| `temperature_kind_schedules` | `temp_kind_schedules` | `kind\x00day_type` | JSON-encoded Ranges |
| `temperature_weekday_defaults` | `temp_weekday` | weekday (1 byte) | day_type string |

Secondary indices (by MAC, by name, by host) are maintained manually in separate buckets — written in the same bbolt `Update` transaction, so consistency is guaranteed.

**Performance characteristics:**
- Point reads: single B-tree traversal, memory-mapped → effectively in-memory speed after first access
- Writes: append-only journal, then commit → single write per `Update()` call
- No interpreter, no WASM, no SQL planner overhead

**Migration effort:** Medium. No library to keep (`sqlx` gone, schema gone). Write thin wrappers that replace the SQL calls with bbolt bucket operations. JSON marshaling already exists on device/config types.

**Pros:**
- Eliminates WASM/interpreter overhead entirely — fastest pure-Go persistence option
- Matches actual access patterns exactly
- Battle-tested, stable API, no breaking changes since v1
- Binary much smaller than SQLite-WASM
- Concurrent readers without locking

**Cons:**
- Manual secondary indices (by MAC, name, host) — must be updated atomically with primary
- `GetDevicesMatchingAny` (substring LIKE) becomes a full bucket scan with Go-side string matching — fine for <200 rows
- Cannot be queried externally with SQL tools (e.g., `sqlite3` CLI)
- Some rewrite of storage layer required

**Verdict:** Best long-term fit. Matches access patterns, eliminates the overhead entirely, pure Go.

---

### Option C — Drop the database: in-memory map + JSON file

All data fits in ~1 MB. Load once at startup into `sync.RWMutex`-protected maps, persist on write by atomically replacing a JSON file (`os.WriteFile` to a temp path, then `os.Rename`).

```go
type Store struct {
    mu      sync.RWMutex
    devices map[deviceKey]*Device   // PK index
    byMAC   map[string]*Device      // secondary
    byName  map[string]*Device      // secondary
    byHost  map[string]*Device      // secondary
    path    string
}
```

**Pros:**
- Zero dependencies added
- All reads are pure memory — fastest possible
- Human-readable persistence file (inspectable, version-controllable if desired)
- Atomic rename gives crash-safe writes
- Matches the project principle: "Simplicity over generality"

**Cons:**
- Entire snapshot written on every change (acceptable at < 1 MB)
- No ACID across multiple structured updates (temperature vs devices in same file) — unless split into two files
- No concurrent access from other processes
- Migration from myhome.db requires one-time export

**Verdict:** Excellent for this scale. Simplest possible, zero runtime overhead. Recommended if the project is unlikely to exceed a few hundred devices.

---

### Option D — badger (github.com/dgraph-io/badger)

LSM-tree KV store, very fast writes, used by DGraph.

**Not recommended here:** badger is optimized for write-heavy workloads with millions of entries. It runs background compaction goroutines, has a complex GC, and its main advantage (write throughput at scale) is irrelevant for config data that changes a few times per hour. Adds complexity and binary size without benefit.

---

### Option E — mattn/go-sqlite3 (CGo SQLite)

The most widely used SQLite Go binding, native C library via CGo.

**Not recommended:** CGo breaks cross-compilation (Raspberry Pi ARM64 from macOS is the stated deployment target per `mac-to-arm64-pipeline` skill). Requires a C compiler in CI. The project is currently CGo-free.

---

## 4. Performance Comparison Summary

| Option | Read latency (est.) | Write latency (est.) | Pure Go | SQL | Effort |
|--------|--------------------|--------------------|---------|-----|--------|
| ncruces/go-sqlite3 (current) | ~100–500 µs | ~1–5 ms | Yes (WASM) | Yes | — |
| modernc.org/sqlite (Option A) | ~20–100 µs | ~200 µs–1 ms | Yes | Yes | Low |
| bbolt (Option B) | ~1–10 µs | ~50–200 µs | Yes | No | Medium |
| In-memory + JSON file (Option C) | < 1 µs | ~1–5 ms (file write) | Yes | No | Medium |
| mattn/go-sqlite3 (Option E) | ~5–20 µs | ~50–200 µs | No (CGo) | Yes | Low |

*Estimates for this data volume (<200 rows). Actual numbers vary by hardware and OS.*

---

## 5. Recommendation

**Phase 1 (quick win, low risk): Option A — switch to modernc.org/sqlite**

Two import lines change. No logic change. Eliminates WASM interpretation overhead immediately. Gives 3–6× better per-query latency. Can be done in under an hour, fully tested with existing tests.

**Phase 2 (right fit, medium effort): Option B — migrate to bbolt**

Replace the SQL storage layer with bbolt buckets. The storage interface (`DeviceStorage`, temperature `Storage`) doesn't change — only the implementation. Eliminates all SQL overhead, matches actual access patterns, reduces binary size. Estimated effort: 1–2 days including tests.

**Skip if simplicity wins: Option C — in-memory + JSON**

If Phase 2 feels over-engineered for a hobby project, Option C is actually the most correct match for the data. < 1 MB of config, load at startup, write a JSON snapshot on change. Zero runtime overhead, zero new dependencies. The argument against it is that bbolt gives you crash-safety and atomic multi-key updates for free without thinking about it.

---

## 6. Measured Benchmark Results (2026-05-09, darwin/amd64 Intel i5-1038NG7)

Three snapshots captured in order: ncruces no-WAL → ncruces+WAL → modernc+WAL.

### Storage package (myhome/storage)

| Benchmark | ncruces no-WAL | ncruces + WAL | modernc + WAL | Total speedup |
|-----------|---------------|---------------|---------------|---------------|
| SetDevice_Insert (in-memory) | 188 µs | 166 µs | **76 µs** | 2.5× |
| SetDevice_Upsert_NoChange | 107 µs | 98 µs | **47 µs** | 2.3× |
| GetDeviceById | 54 µs | 46 µs | **22 µs** | 2.5× |
| GetDeviceByMAC | 52 µs | 43 µs | **20 µs** | 2.6× |
| GetAllDevices_20 | 131 µs | 112 µs | **90 µs** | 1.5× |
| SetDevice_File_Insert | 1,160 µs | 203 µs | **118 µs** | **9.8×** |
| SetDevice_File_Upsert_NoChange | 155 µs | 103 µs | **57 µs** | 2.7× |

WAL alone accounts for 5.7× on file writes. modernc adds another 1.7× on top.

### Temperature package (myhome/temperature)

| Benchmark | ncruces no-WAL | modernc + WAL | Speedup |
|-----------|---------------|---------------|---------|
| SaveRoom_Insert | 62 µs | **26 µs** | 2.4× |
| SaveRoom_NoChange | 59 µs | **24 µs** | 2.5× |
| GetRoom | 47 µs | **18 µs** | 2.6× |
| ListRooms_10 | 99 µs | **61 µs** | 1.6× |
| SetKindSchedule_Upsert | 43 µs | **17 µs** | 2.5× |
| GetKindSchedules_All | 95 µs | **65 µs** | 1.5× |

### Crash note

The ncruces wasm heap is capped at ~16 MB. Running benchmarks with unbounded inserts (b.N = 21k+) caused a wasm `out of bounds memory access` panic in temperature tests. The benchmarks were fixed to cycle through a bounded pool of IDs (200 devices, 20 rooms). modernc has no such constraint.

---

## 7. Implementation Plan (if Option B chosen)

### Phase 1 — modernc.org/sqlite swap (can do immediately)

- [ ] In `myhome/storage/go.mod`: replace `ncruces/go-sqlite3` with `modernc.org/sqlite`
- [ ] In `myhome/storage/db.go`: replace driver imports, keep everything else
- [ ] In `myhome/temperature/go.mod` and `storage.go`: same swap
- [ ] `make test` — existing tests should pass unchanged
- [ ] Measure startup time and per-operation latency with `go test -bench`

### Phase 2 — bbolt migration

#### 2a. Define storage interface (if not already)
- Define `DeviceStore` and `TemperatureStore` interfaces so both SQL and bbolt implementations can coexist during migration

#### 2b. bbolt storage implementation — devices
- File: `myhome/storage/bbolt.go`
- Buckets: `devices`, `devices_by_mac`, `devices_by_name`, `devices_by_host`, `devices_by_room`
- Implement all `DeviceStorage` methods using bbolt transactions
- Secondary indices updated atomically in same `Update()` transaction

#### 2c. bbolt storage implementation — temperature
- File: `myhome/temperature/bbolt.go`
- Buckets: `temp_rooms`, `temp_kind_schedules`, `temp_weekday_defaults`

#### 2d. Migration from myhome.db
- On first run with new binary: if `myhome.db` exists and `myhome.bbolt` does not, read all devices from SQLite and write into bbolt, then rename `myhome.db` to `myhome.db.bak`
- One-time migration, no user action required

#### 2e. Remove SQLite dependencies
- Remove `ncruces/go-sqlite3` (or `modernc.org/sqlite`) and `jmoiron/sqlx` from both modules

#### 2f. Validation
- All existing `db_test.go` tests pass (implementation-agnostic assertions)
- `make test` clean
- Manual: start daemon, discover devices, restart, verify devices persist

---

## 8. Open Questions

1. **Cross-platform file path:** `myhome.db` is relative to CWD. Same issue exists for bbolt. Should the DB path be configurable? (Currently it is — passed as `dbName` param from daemon.go.)
2. **`myhome ctl db export/import`:** These commands read the SQLite file directly (`sqlx`). They need updating regardless of which option is chosen.
3. **WAL mode not enabled:** Even if staying on SQLite, enabling `PRAGMA journal_mode=WAL` and `PRAGMA synchronous=NORMAL` would give a significant write speedup with no code change — quick win regardless.
4. **Temperature in-memory cache:** `myhome/temperature` already has a `sync.RWMutex` protected in-memory cache layered over the DB. This pattern is already most of Option C — the DB is really just the startup loader and crash-safe persistence. This reinforces that a simpler persistence layer is sufficient.
