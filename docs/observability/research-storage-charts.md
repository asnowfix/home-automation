# Research: time-series storage and charting for the myhome daemon on a Raspberry Pi 3B+

**Status:** complete (2026-11-01). Sections 0-5 and the summary are written. Measurements were on this workstation, not the Pi.
**Question:** What is the right time-series storage and charting approach for a Go daemon on a
Raspberry Pi 3B+ (aarch64, 906 MB RAM, ~580 MB available, SD card)?
**Owner constraints (from dispatch):** start with native charts in the existing htmx/Alpine/Bulma UI,
extended later; default 1 week of metric retention, configurable; if the monitoring stack lives inside
the daemon, storage must be SQLite; daemon restarts every 6 h (`RuntimeMaxSec=21600`); Prometheus 2.42
is installed on the NAS but monitors no device; NAS port 9100 is held by node-exporter.
**Constraints:** no production code, no implementation plan, no live-system access.

**Labels used below:** `[VERIFIED]` = read in a primary source, or reproduced locally, with the source or
command given. `[MEASURED]` = my own run on this workstation (not the Pi). `[ESTIMATE]` = my arithmetic or
judgement from verified inputs; the assumptions are stated. `[NOT ESTABLISHED]` = the sources did not say,
or I could not check.

---

## 0. Local facts (repository at worktree `feature+observability`, HEAD `33be2480`)

| Fact | Evidence |
|---|---|
| SQLite driver in the daemon's stores is **`modernc.org/sqlite v1.50.0`** (pure Go, no CGo). Used by `myhome/events`, `myhome/storage`, `myhome/temperature`, `myhome/vacuum`, `myhome/fetchproxy`. | `myhome/events/go.mod`, `myhome/storage/go.mod` etc. `[VERIFIED]` |
| The bundled SQLite library is **3.53.0**. | `modernc.org/sqlite@v1.50.0/lib/sqlite_linux_amd64.go` (`SQLITE_VERSION = "3.53.0"`); CHANGELOG entry "v1.49.0: Upgrade to SQLite 3.53.0" (module cache copy at the Go module cache, `modernc.org/sqlite@v1.50.0`). `[VERIFIED]` |
| Older `ncruces/go-sqlite3` (WASM) still appears as an indirect requirement in `internal/myhome/go.mod`; `docs/EMBED-DB-PLAN.md` records the switch to modernc on branch `feature+db-provider`. | `internal/myhome/go.mod`, `docs/EMBED-DB-PLAN.md`. `[VERIFIED]` |
| Event store pragmas: `journal_mode=WAL`, `synchronous=NORMAL`, `SetMaxOpenConns(1)`. **No `busy_timeout`, `wal_autocheckpoint`, `auto_vacuum` or `cache_size` is set anywhere** in the Go code. | `myhome/events/storage.go` (about lines 55-80); grep of `myhome/` and `internal/` for those pragma names returned nothing. `[VERIFIED]` |
| Event retention: a ticker runs **hourly** and calls `DELETE FROM events WHERE ts < ?` (default 90 days, `--events-retention`). | `myhome/events/service.go` (`time.NewTicker(time.Hour)`), `myhome/events/storage.go` `Purge()`, `myhome/daemon/run.go:77`. `[VERIFIED]` |
| Events schema has `UNIQUE (device_id, component, event, ts)` plus 4 secondary indexes. A `sensor_daily_stats` table already holds per-day min/max/sum/samples, i.e. a rollup pattern is already in use. | `myhome/events/storage.go` `createTables()`. `[VERIFIED]` |
| No time-series store exists today. | grep. `[VERIFIED]` |
| **No Prometheus client library is a dependency.** No `client_golang` in any `go.mod` or `go.sum`. The exporter writes text format by string concatenation. | grep over all `go.mod`/`go.sum`. `[VERIFIED]` |
| Metrics exporter is **off by default**: `--enable-metrics-exporter` defaults to `false`, although the code comment says "auto-enabled with device manager". | `myhome/daemon/run.go:69`, `myhome/daemon/daemon.go:203-225`. `[VERIFIED]` |
| Exporter default port is **9100** (`options.PROMETHEUS_DEFAULT_PORT`); the generated scrape config defaults to `myhome.local:9100`. | `myhome/ctl/options/options.go:23`, `myhome/ctl/shelly/script/prometheus.go:12`. `[VERIFIED]` |
| Devices publish metrics every **30 s** (`publishIntervalSeconds: 30`), QoS 1, not retained, to `shelly/metrics/<device-id>`, as Prometheus text. | `internal/shelly/scripts/prometheus-metrics.js` (CONFIG block, `publishMetrics()`). `[VERIFIED]` |
| The `watchdog.js` header says it does Prometheus metrics, but the publishing code is in `prometheus-metrics.js` (commit `9db5c97c`, #268). The header comment is stale. | `internal/shelly/scripts/watchdog.js` lines 1-5; `prometheus-metrics.js`. `[VERIFIED]` |
| The repo's scrape-side docs describe a Debian `prometheus-mqtt-exporter` (port 8079) for individual JSON metrics, and myhome's own exporter (port 9100) for the text payload. Two paths. | `docs/prometheus-mqtt-exporter.md`. `[VERIFIED]` |
| Project licence is **Mozilla Public License 2.0** (not Apache). | `LICENSE` at worktree root (first line "Mozilla Public License Version 2.0"). `[VERIFIED]` |

Local tools used for checks: `promtool` and `prometheus` **2.53.5** (Debian `2.53.5+ds1`), not the NAS's 2.42.
Module cache: `prometheus/common@v0.70.0`, `client_golang@v1.23.2`. `python3` 3.x stdlib `sqlite3` reports
SQLite **3.46.1**.

---

## 1. SQLite as a time-series store

### 1.1 Primary-source facts (fetched 2026-11-01)

| Topic | Fact (quoted where exact) | Source |
|---|---|---|
| WAL concurrency | "WAL provides more concurrency as readers do not block writers and a writer does not block readers." | https://sqlite.org/wal.html (page updated 2026-08-25 per page text) |
| WAL, same host | "All processes using a database must be on the same host computer; WAL does not work over a network filesystem." | https://sqlite.org/wal.html |
| WAL, transaction size | "WAL works best with smaller transactions." Since 3.11.0 (2016-02-15) large transactions are about as efficient as in rollback mode. | https://sqlite.org/wal.html |
| Autocheckpoint | "By default, SQLite does a checkpoint automatically when the WAL file reaches a threshold size of 1000 pages." | https://sqlite.org/wal.html |
| WAL file size | Checkpoints "do not normally truncate the WAL file"; in normal operation the WAL "grows to about 1000 pages (about 4MB)". It can grow without bound if autocheckpoint is disabled, under checkpoint starvation by overlapping readers, or with very large write transactions. | https://sqlite.org/wal.html |
| WAL corruption bug | Affects 3.7.0 through 3.51.2 (two connections checkpointing or writing at the same instant); fixed in 3.51.3 (2026-03-13). The bundled SQLite in `modernc.org/sqlite v1.50.0` is 3.53.0. | https://sqlite.org/wal.html; `modernc.org/sqlite@v1.50.0/lib/sqlite_linux_amd64.go` |
| synchronous=NORMAL | "WAL mode is always consistent with synchronous=NORMAL, but WAL mode does lose durability." "A transaction committed in WAL mode with synchronous=NORMAL might roll back following a power loss or system crash." Application crashes do not lose committed transactions. | https://sqlite.org/pragma.html |
| synchronous=NORMAL, syncs | With NORMAL in WAL, "the checkpoint is the only operation that issues a sync." | https://sqlite.org/wal.html |
| busy_timeout | No default stated. "Each database connection can only have a single busy handler." | https://sqlite.org/pragma.html |
| auto_vacuum | Default NONE. INCREMENTAL does not vacuum at commit; `incremental_vacuum(N)` removes up to N free pages. Changing NONE to another mode needs VACUUM on an empty DB. | https://sqlite.org/pragma.html |
| page_size | Default 4096 since SQLite 3.12.0 (2016-03-29). | https://sqlite.org/pragma.html |
| cache_size | Default -2000 (about 2 MB). | https://sqlite.org/pragma.html |
| WITHOUT ROWID | "likely to be helpful for tables that have non-integer or composite (multi-column) PRIMARY KEYs". "a WITHOUT ROWID table can use about half the amount of disk space and can operate nearly twice as fast" (a best case). Rule of thumb: average row "less than about 1/20th the size of a database page" (about 200 B at 4 KiB). Not advantageous for single-column INTEGER primary keys or large rows. | https://sqlite.org/withoutrowid.html |
| WITHOUT ROWID limits | Must have a PRIMARY KEY; AUTOINCREMENT and `sqlite3_last_insert_rowid()` do not work with it. | https://sqlite.org/withoutrowid.html |
| Prometheus density | "Prometheus stores an average of only 1-2 bytes per sample." Default retention `15d`. Disk formula: `needed_disk_space = retention_time_seconds * ingested_samples_per_second * bytes_per_sample`. | https://prometheus.io/docs/prometheus/latest/storage/ |
| Linux write counter | `/proc/diskstats` field 7 is "# of sectors written". The fetched page does not state the sector size. **Unit NOT ESTABLISHED from that source.** | https://www.kernel.org/doc/html/latest/admin-guide/iostats.html |

**SD-card endurance: NOT ESTABLISHED.** I found no primary source giving SD-card endurance (TBW) or
write-amplification figures. Forum posts say SQLite's per-commit syncs drive flash writes, and that power loss
corrupts SD cards more often than wear does. These are anecdotes, not numbers: https://community.home-assistant.io/t/raspberry-pi-filesystem-stability/21302/16 and https://discourse.nodered.org/t/influxdb-wal-and-sd-cards/27575 (seen in search results 2026-11-01, not fetched in full).

### 1.2 Measured on this workstation (not the Pi)

**Method.** Python 3.46.1 `sqlite3` (SQLite 3.46.1, page size 4096, WAL, `synchronous=NORMAL`), one connection,
one transaction per simulated minute, 400 rows per transaction (40 devices x 10 series). 7 days = 10,080 commits
and **4,032,000 rows**. Values are random floats (uniform 0-3000, two decimals), which is close to worst case for
compression. Harness (throwaway, not production): `/tmp/claude-1004/-home-fix-GIT-home-automation/39763477-6882-4b9f-922f-86bfaa818ace/scratchpad/sqlite_measure.py`.
`app write` = bytes passed to `write()` by the process (`/proc/self/io` `wchar`). It includes WAL frames and
checkpoint copies. It does **not** include fsync counts or what the card's flash controller does internally.

| Schema | Key / index | DB file, 7 d, no deletes | Bytes per row (file) | App-level writes, 7 d | Per day |
|---|---|---|---|---|---|
| A | rowid table + secondary index on `(series_id, ts)` | 161.4 MiB | 41.98 | 26,319 MiB | ~3,760 MiB (3.67 GiB) |
| B | `WITHOUT ROWID`, PK `(series_id, ts)` | 89.2 MiB | 23.19 | 28,359 MiB | ~4,051 MiB (3.96 GiB) |
| C | `WITHOUT ROWID`, PK `(ts, series_id)` | 90.7 MiB | 23.58 | 392.9 MiB | ~56 MiB |
| D | rowid table, no index (append-only) | 93.3 MiB | 24.27 | 311.8 MiB | ~45 MiB |

Source file for the numbers: the run output of the harness above (single run per schema).

What the table shows:
- **Row cost is about 23-24 bytes per sample** for uncompressed SQLite storage of
  `(ts INTEGER, series INTEGER, value REAL)`. That is roughly 12-24 times Prometheus's "1-2 bytes per sample".
  The comparison is not like-for-like: Prometheus compresses regular timestamps and repeated values, and my test
  values are random. Real sensor series would compress better in Prometheus; SQLite does no compression here.
- **Key order and secondary indexes drive write volume far more than row size.** Schemas A and B touch about 400
  different B-tree leaves per commit, because each minute's 400 rows fall at 400 series-major positions. Each commit
  therefore rewrites many 4 KiB pages into the WAL. Schema C keeps each minute's rows adjacent. Ratios on this run:
  A is about 67x C, and B is about 72x C. `[MEASURED, one run each]`
- Schema D (rowid, append-only, no index) is nearly as cheap as C. C gives the same write cost plus a clustered
  primary key for time-range scans. `[MEASURED]`

### 1.3 Rolling 7-day retention (schema C, one `DELETE` per simulated day of rows older than 7 days)

Harness: `/tmp/claude-1004/-home-fix-GIT-home-automation/39763477-6882-4b9f-922f-86bfaa818ace/scratchpad/sqlite_retention.py` (throwaway). 14 simulated days. Each day inserts 576,000 rows, then deletes 576,000 rows.

| Mode | Day 7 file | Steady-state file (day 8-14) | Freelist pages (steady) | WAL peak / steady | App-level writes, 14 d | Per day |
|---|---|---|---|---|---|---|
| `auto_vacuum=NONE` | 90.4 MiB | 103.6 MiB | ~3,315 (about 13 MiB, reused) | 14.3 MiB | 1,003.2 MiB | ~72 MiB |
| `auto_vacuum=INCREMENTAL`, `PRAGMA incremental_vacuum` after each delete | 90.3 MiB | 90.8 MiB | 0 | 16.3 MiB | 1,238.8 MiB | ~88 MiB |

- With NONE, the file settles at about 104 MiB. Freed pages are reused, so the file never shrinks. `[MEASURED]`
- With INCREMENTAL plus `incremental_vacuum`, the file returns to about 91 MiB and the freelist is empty.
  The cost is about 236 MiB more app-level writes over 14 days (about 17 MiB/day). `[MEASURED]`
- The WAL grows to about 14-16 MiB on delete days because a whole day's delete is one transaction. Above the
  4 MiB autocheckpoint size, as the SQLite docs predict. `[MEASURED]`
- Note: my first INCREMENTAL attempt did not step the `incremental_vacuum` statement to completion (a harness
  bug: no `fetchall()`). Its output showed a 104 MiB file with freelist, so I discarded it and re-ran. The numbers
  above are from the corrected run.

### 1.4 Estimates (arithmetic; not measured on the Pi)

Inputs: 40 devices x 10 series = 400 series. At 1 sample per series per minute: 400 x 1,440 = 576,000
samples/day, x 7 = **4,032,000 samples per week**.

- **Raw data, one week:** 4,032,000 x 23.58 B (measured C) = 95.1 MB = 90.7 MiB. This matches the measured file.
- **Steady-state file with 7-day retention:** about 104 MiB (NONE) or 91 MiB (INCREMENTAL). `[MEASURED]`
  Budget about 110-120 MiB on disk for headroom `[ESTIMATE]`. Disk size is not the constraint; write rate is.
- **Daily application writes at 1-minute resolution, ts-major key:** about 56 MiB/day for inserts (measured C)
  plus about 16 MiB/day for deletes, about 72 MiB/day in total `[MEASURED on this host; ESTIMATE for the Pi]`.
  That is about 25 GiB per year before the card's internal write amplification, which is NOT ESTABLISHED.
- **Same workload with series-major key or a secondary index:** about 3.7-4.0 GiB per day (A and B). This is a
  design fact from the measurement, not an extrapolation.
- **Rollups (estimate):** a 1-hour rollup for 400 series is 400 x 24 = 9,600 rows/day, or 67,200 per week. At an
  assumed ~50 bytes per row for 4-5 aggregate columns `[ESTIMATE, not measured]`, that is about 3 MB per week,
  or about 175 MB for 1 year of hourly rows (400 x 8,760 = 3.5 M rows). The existing `sensor_daily_stats` table in
  `events.db` already uses this per-period aggregate shape (section 0).
- **Batching:** the measurements committed once per simulated minute. Longer batching was NOT measured.
  Fewer, larger commits would reduce pages written per row, but by how much is NOT ESTABLISHED.

### 1.5 What must be measured on the Pi (not done; no Pi access)

1. Card writes per day: `/proc/diskstats` field 7 (sectors written) before and after 24 h of the real workload,
   with the daemon running and stopped. Confirm the sector unit from kernel source before converting to bytes.
2. Application writes: `/proc/<pid>/io` `wchar` and `write_bytes` for the daemon over the same 24 h.
3. Insert time and CPU of the 1-minute batch on the Cortex-A53 at 1.4 GHz, with `modernc.org/sqlite` (native Go;
   the WASM driver used by older stores is slower, see `docs/EMBED-DB-PLAN.md`).
4. Retention delete duration and WAL peak on the Pi.
5. RSS and heap of the daemon with the metrics store enabled, against the roughly 580 MB available.
6. Card endurance and any write-amplification figure, from the card's datasheet. NOT ESTABLISHED.

---

## 3. Prometheus exposition at `/metrics`: the current `myhome/metrics/exporter.go`

### 3.1 What the code does (verified by reading `myhome/metrics/exporter.go`, 187 lines)

- `MetricsCache` is `map[deviceID]string`. `Set()` overwrites a device's payload. **There is no delete, no TTL,
  and no timestamp.** `[VERIFIED]`
- `GetAll()` builds the response with `result += metrics` over the Go map, so device order is random per scrape.
  Each device's payload is appended verbatim. `[VERIFIED]`
- `handleMetrics` serves `text/plain; version=0.0.4; charset=utf-8` with that concatenation. No `client_golang`
  registry, no `promhttp`, no process or Go collectors, no `scrape_duration_seconds`, no per-device `up`. `[VERIFIED]`
- Subscribes to `<topic>/#` (default topic `shelly/metrics`, buffer 8). Device ID is the topic suffix. `[VERIFIED]`
- `/health` returns `{"status":"ok"}` or `{"status":"mqtt_disconnected"}`, reflecting MQTT connectivity only. `[VERIFIED]`
- Daemon wiring at `myhome/daemon/daemon.go:203-225`, off unless `--enable-metrics-exporter`. `[VERIFIED]`

### 3.2 Concrete defects and findings

**D1. Concatenated payloads violate the exposition format (verified locally).**
The exposition-format page (https://prometheus.io/docs/instrumenting/exposition_formats/, fetched 2026-11-01) says:
- "Only one HELP line may exist for any given metric name."
- "Only one TYPE line may exist for a given metric name."
- "All lines for a given metric must be provided as one single group, with the optional HELP and TYPE lines first."

Each device's script emits its own `# HELP`/`# TYPE` block for the same metric names (`shelly_uptime_seconds`,
`shelly_switch_output`, ...). `GetAll()` joins N of these, so each name has N HELP/TYPE pairs, and the groups
are not contiguous.

Reproduction (local, scratch files only, no device or network involved):
- `promtool check metrics` on a **single**-device payload: parses. One lint warning only:
  `shelly_uptime_seconds counter metrics should have "_total" suffix`. Exit 3.
- `promtool check metrics` on the **two-device concatenation** (the `GetAll` shape):
  `error while linting: text format parsing error in line 9: second HELP line for metric name "shelly_uptime_seconds"`. Exit 1.
  The strict parser is `prometheus/common` `expfmt/text_parse.go` (`second HELP line for metric name` at line 648,
  `second TYPE line ...` at line 663, v0.70.0).
- **Prometheus server 2.53.5 (local throwaway instance, scraping a localhost file server) accepted the same
  concatenated payload.** Target `up=1`, no `lastError`, and both `dev-a` and `dev-b` series were stored
  (`shelly_uptime_seconds{id="dev-b"} 200`). So the server's own parser is more lenient than `expfmt`.
  **Not tested: Prometheus 2.42, the NAS version.** `[VERIFIED for 2.53.5 only]`

Conclusion: the output violates the spec and is rejected by strict consumers (promtool, `expfmt`-based clients).
The local Prometheus 2.53.5 tolerated it, but the spec is explicit, so do not rely on that tolerance.

**D2. Offline devices are served forever with their last values (verified from code; not tested live).**
`MetricsCache` never evicts. A device that stops publishing keeps its last payload in `/metrics` indefinitely.
Prometheus therefore sees a successful scrape with the frozen values every time, and a dead device looks live.
Nothing in the exporter distinguishes "last seen 10 s ago" from "last seen 3 days ago". The exposition-format
page does not describe staleness ("The document does not mention staleness."). A series is marked stale by
Prometheus when it disappears from a scrape, and this exporter never stops emitting the series.
Sources: `myhome/metrics/exporter.go` `Set()` and `GetAll()`; exposition-format page above.

**D3. No per-device liveness series.** There is no per-device `up`, no `last_seen` timestamp and no
`device_online` gauge. The only liveness hint is `shelly_uptime_seconds`, which is itself frozen once the device
stops publishing (D2). `[VERIFIED from code]`

**D4. Counter semantics across restarts (partial; verified from the device script).**
- `shelly_uptime_seconds` is typed `counter`. It resets to near zero on device reboot. That is a normal
  counter reset, which `rate()` and `increase()` handle. It is not monotonic. `[VERIFIED from prometheus-metrics.js]`
- `activated`/`deactivated` counts are kept in the script's memory (`activationCounts`, `deactivationCounts`)
  and reset on device reboot or script restart. They are published only on the individual MQTT topics
  (`shelly/<id>/switch_N_activated`). They are **not** in the Prometheus text payload, so `/metrics` does not
  carry them, despite the names in `docs/prometheus-mqtt-exporter.md`. `[VERIFIED from prometheus-metrics.js;
  grep of `printPrometheusMetric` calls contains no `activated`]`
- The daemon owns no counters, so its own 6-hourly restart does not reset any exported counter. The cache is
  empty after each restart and fills within one publish interval (30 s), so scrapes in that window see no device
  series. `[ESTIMATE from the 30 s interval and an empty cache at start]`
- `shelly_uptime_seconds` is typed `counter` without the `_total` suffix; promtool lints it. `[VERIFIED promtool output]`

**D5. Other notes (verified from code, not observed live).**
- `printPrometheusMetric` writes `String(value)`. If `sys.uptime` or a sensor value is missing, the output contains
  the literal `undefined`, which a parser would reject. Code path only; live behaviour NOT ESTABLISHED.
- `switch_power_total` help text says "watts hours" (sic).
- `GetAll()` concatenates with `+=` in a loop. Quadratic copying, harmless at this scale.
- Port 9100 collides with node-exporter on the NAS only if the daemon runs there. The user states node-exporter
  holds 9100 on the NAS. I did not verify that (no live access). `[NOT ESTABLISHED by me]`

### 3.3 What is reusable

- Device-side metric names and labels (`name`, `id`, `mac`, `app`, plus `switch` on switch series) are a sensible
  shape. Each series carries the device `id`, so per-device filtering works once D1 is fixed. `[VERIFIED from prometheus-metrics.js]`
- The MQTT subscription pattern (`<topic>/#`, device ID from the topic suffix) is reusable. `[VERIFIED]`
- `/health` as a daemon liveness probe is reusable. `[VERIFIED]`

### 3.4 Not established (section 3)
- Prometheus 2.42 behaviour on the duplicate-HELP payload (only 2.53.5 was tested).
- Live behaviour of the exporter against the real fleet (no live access; not done).
- Whether node-exporter on the NAS binds 9100 on all interfaces (user-stated; not checked).


---

## 2. Embedded or self-hosted time-series engines: Prometheus TSDB, VictoriaMetrics, tstorage (and SQLite, from section 1)

Facts are from the sources in the table; Pi-specific RAM figures were **not** established for any of them,
because I had no Pi and found no primary source with a RAM-per-series figure.

| Option | Licence | Latest release / maintenance (as read 2026-11-01) | Embeddable from Go? | ARM / Pi 3B+ | Disk density | RAM |
|---|---|---|---|---|---|---|
| **Prometheus** server (TSDB inside) | Apache-2.0 (the `tsdb` package in the same repo is stated as "License: Apache-2.0", https://pkg.go.dev/github.com/prometheus/prometheus/tsdb) | 3.15.1 released 2026-10-21, labelled "Latest" (https://prometheus.io/download/) | Yes, as a library: the README says "This directory contains the Prometheus TSDB (Time Series DataBase) library" (same pkg.go.dev page). No API-stability statement found. | Download page lists **only `linux-amd64`** (https://prometheus.io/download/). ARM would need a distro package or a source build; NOT ESTABLISHED which distro packages exist for arm64. | "Prometheus stores an average of only 1-2 bytes per sample." (https://prometheus.io/docs/prometheus/latest/storage/) | Docs give **no** RAM-per-series figure (verified: the storage page has none). A vendor blog says ~3 KB per active series (CubeAPM, unverified). At 400 series that would be ~1.2 MB of head-series memory `[ESTIMATE from an unverified blog figure]`; process baseline NOT ESTABLISHED. |
| **VictoriaMetrics** single-node | Apache-2.0 (https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/ and GitHub README) | v1.155.0 "Latest", released 2026-10-23 (https://github.com/VictoriaMetrics/VictoriaMetrics/releases/latest) | **Not documented.** The README and single-node docs say nothing about embedding. The module tree has `lib/` packages (file listing only, no stability statement). Feasible in principle; unsupported. | Docs: "ARM build may run on Raspberry Pi or on energy-efficient ARM servers." The Makefile has `victoria-metrics-linux-arm` and `-arm64` **build** targets; whether arm binaries are published was NOT established (the README names no architectures). | Docs claim "up to 7x less storage space" than Prometheus (vendor claim). No bytes-per-sample figure in the fetched sections. | Docs claim "up to 7x less RAM than Prometheus, Thanos or Cortex" (vendor claim). No Pi figure. Recommends keeping "50% of free RAM" spare. Default retention 1 month (min 24 h). |
| **tstorage** (`github.com/nakabonne/tstorage`) | Apache-2.0 (GitHub API `license.spdx_id`) | Last push 2026-03-16; not archived; 1,255 stars, 14 open issues (GitHub API, 2026-11-01). | Yes. "An embedded time-series database" (repo About). Requires Go 1.16+. | Not stated. | Not stated ("compressed, read-only and memory-mapped" disk partitions; no ratio given). | In-memory by default: "The memory partition is writable and stores data points in heap." Disk partitions are mmap'd. Heap size for 400 series at 1/min is NOT ESTABLISHED. |
| **SQLite** (this project's driver, `modernc.org/sqlite`) | NOT CHECKED in this pass (SQLite licence and modernc driver licence were not fetched) | SQLite 3.53.0 bundled in v1.50.0 (module cache). | Already used. | Pure Go; no CGo. | **Measured 23-24 bytes per row**, uncompressed (section 1.2). | Not measured on the Pi. |

Notes that follow from the table:
- At 400 series x 1/min over 7 days (4,032,000 samples), the Prometheus claim of 1-2 bytes per sample implies
  about 4-8 MB per week, against 90.7 MiB measured for SQLite. The difference is real (roughly 10-20x) but it is
  a comparison of a vendor-documented figure against random-value synthetic data (section 1.2), not a like-for-like
  measurement. `[ESTIMATE]`
- The write pattern of a TSDB on an SD card (Prometheus head-block WAL and compaction; VictoriaMetrics merges) was
  NOT measured. The SQLite write numbers in section 1 are the only write-rate measurements in this file.
- InfluxDB, TimescaleDB and other engines were NOT researched in this pass.

**Verdict for section 2 (observation, not a plan).** The owner's SQLite constraint is compatible with this
workload size, because the measured weekly footprint is about 91-104 MiB. The main cost is write rate, which is
set by key order and indexes (section 1.2), not by storage engine choice. An external TSDB would add a second
process and an ARM build question, and neither was verified for the Pi.

---

## 4. Charting for an htmx/Alpine UI with embedded assets and no CDN at runtime

### 4.1 Measured bundle sizes (npm tarballs, latest versions, 2026-11-01)

Method: `curl` of each npm tarball from `https://registry.npmjs.org/<pkg>/-/<pkg>-<version>.tgz`, extracted to
its own scratch directory, then gzip level 9 of the browser bundle as shipped. Minified bytes, not HTTP
transfer size (which depends on the server's gzip or brotli). Browser bundle paths are as listed.

| Library (version) | Licence (npm `license`) | Latest publish date (npm `time`) | Bundle | Raw bytes | gzip-9 bytes |
|---|---|---|---|---|---|
| **uPlot** 1.6.32 | MIT (GitHub README: "MIT license") | 2025-03-14 | `dist/uPlot.iife.min.js` | 51,081 | 21,991 |
| uPlot CSS | MIT | 2025-03-14 | `dist/uPlot.min.css` | 1,857 | 758 |
| **Chart.js** 4.6.1 | MIT (GitHub: "available under the MIT license") | 2026-10-22 | `dist/chart.umd.min.js` | 210,082 | 71,003 |
| **ECharts** 6.1.0 | Apache-2.0 (GitHub: "available under the Apache License V2") | 2026-05-19 | `dist/echarts.min.js` (full) | 1,121,883 | 368,202 |
| ECharts (simple) | Apache-2.0 | 2026-05-19 | `dist/echarts.simple.min.js` | 500,315 | 169,097 |
| ECharts (common) | Apache-2.0 | 2026-05-19 | `dist/echarts.common.min.js` | 715,020 | 239,805 |
| **Dygraphs** 2.2.2 | MIT (npm `license`) | 2026-07-27 | `dist/dygraph.min.js` | 130,226 | 37,259 |
| Dygraphs CSS | MIT | 2026-07-27 | `dist/dygraph.min.css` | 1,255 | 439 |

Sources: npm registry metadata `https://registry.npmjs.org/uplot/latest`, `.../chart.js/latest`,
`.../echarts/latest`, `.../dygraphs/latest` (each has `license`, `version`, and the publish time in the
full metadata `time` map). GitHub pages https://github.com/leeoniya/uPlot, https://github.com/chartjs/Chart.js,
https://github.com/apache/echarts (licence and description text, fetched 2026-11-01).

Chart.js declares a runtime dependency on `@kurkle/color` in `package.json`. I did not measure whether the UMD
bundle includes it. ECharts' "common" and "simple" builds are the documented smaller entry points (npm
`dist/` file names; the ECharts page did not describe them).

### 4.2 Time-series suitability and performance claims (documentation only; NOT measured in a browser)

- **uPlot.** Canvas 2D: "fast, memory-efficient Canvas 2D-based chart" (GitHub). Feature list includes
  "Temporal or numeric x-axis" and "Support for IANA Time Zone Names & DST" (GitHub). Author's benchmark
  (Chrome 113 per the README): "From a cold start it can create an interactive chart containing 166,650 data
  points in 25ms", and "In most sane cases, you can live-stream data with uPlot at 60fps, though it may begin to
  struggle beyond 100k in-view points." These are the author's numbers, not independent ones.
- **Chart.js.** Canvas: "Simple HTML5 Charts using the <canvas> tag." Time scale: "The time scale requires both a
  date library and a corresponding adapter to be present." The page does not name the adapter it requires
  (https://www.chartjs.org/docs/latest/axes/cartesian/time.html). Decimation plugin with `min-max` and `lttb`
  algorithms, whose purpose is "to reduce the number of points drawn on the graph for improved performance"
  (https://www.chartjs.org/docs/latest/samples/advanced/data-decimation.html). The page does not give
  performance numbers, nor whether decimation requires `parsing: false`.
- **ECharts.** Renderer is canvas (zrender); SVG is also available. The renderer page
  (https://echarts.apache.org/handbook/en/best-practices/canvas-vs-svg/) says: "Canvas is more suitable for
  charts with a large number of elements" with ">1k" as a rough threshold; SVG "has less memory usage (which is
  important for mobile devices)"; and "the SVG renderer may work better if the chart is running on a low-end
  Android". The page does not describe `large` mode, sampling or progressive rendering.
- **Dygraphs.** npm keywords include "canvas" and "timeseries"; description "dygraphs is a fast, flexible open
  source JavaScript charting library" (npm metadata). I did not fetch its performance documentation.

### 4.3 Licence compatibility

The project's own `LICENSE` is **Mozilla Public License 2.0** (verified at the worktree root), not Apache-2.0. All
four candidates are MIT or Apache-2.0, which are permissive licences. I am not giving a legal opinion on
inclusion. The point to check is that the chosen library's licence is recorded alongside its pinned asset, as the
existing UI does.

### 4.4 How the existing UI brings in JavaScript (local, verified)

`internal/myhome/ui/static/main.go` pins each third-party asset with a `go:generate fetch` line that includes a
URL and a SHA-256 (lines 5-22: bulma 0.9.4, htmx 2.0.4, alpinejs 3.14.3), and embeds the result with `//go:embed`.
A new chart library would follow the same mechanism. This is an observation about the current pattern, not a plan.
The `fetch` helper is `cmd/fetchasset` (path in the `go:generate` line).

### 4.5 Recommendation (for the native-chart phase)

**uPlot** is the best fit on the evidence here. It is the smallest (22 KB gzip for the IIFE bundle), it is canvas
based, its x-axis supports time natively, and it has no date-adapter requirement (the Chart.js time scale does).
It is MIT. Its author reports the numbers above for large point counts. Two caveats:
- **Maintenance:** the latest npm release is 2025-03-14, about 20 months before this research date. If that matters,
  Dygraphs (MIT, published 2026-07-27, 37 KB gzip) is the time-series alternative, at about 1.7x the size.
- **Feature depth:** I did not compare features, legends, tooltips or interactions across the four libraries.
  ECharts and Chart.js have far larger feature sets, and ECharts is the heaviest (368 KB gzip full build).

Client performance on a "low-end client" was NOT measured. The Pi serves the assets; the browser on the phone or
laptop renders the chart.

---

## 5. Later extension path: Grafana vs Perses

### 5.1 Grafana

- **Licence:** GNU Affero General Public License v3.0 (`LICENSE` file, https://github.com/grafana/grafana/blob/main/LICENSE, fetched 2026-11-01).
- **Hardware (official):** "Minimum recommended memory: 512 MB", "Minimum recommended CPU: 1 core", and the FAQ calls
  this "a floor for evaluation rather than a production target". The tier table lists Small as 2 cores, 2-4 GB memory,
  10-20 GB SSD; Medium as 4-8 cores, 8-16 GB (https://grafana.com/docs/grafana/latest/installation/requirements/ ;
  the `setup-grafana/requirements` URL I first tried returned 404).
- **Supported OS:** Debian or Ubuntu, RHEL or Fedora, SUSE or openSUSE, macOS, Windows. "Installation of Grafana on
  other operating systems is possible, but is not recommended or supported." CPU architectures are **not listed**
  on that page. Raspberry Pi / arm64 support: NOT ESTABLISHED from official docs.
- **Database:** "By default Grafana uses an embedded SQLite database". "SQLite isn't recommended for production
  environments." High availability requires MySQL or PostgreSQL (https://grafana.com/docs/grafana/latest/setup-grafana/installation/ and the requirements page).
- **RAM on a Pi 3B+:** NOT ESTABLISHED. The 512 MB figure is an evaluation floor, which leaves little margin in
  the roughly 580 MB the daemon already has available.

### 5.2 Perses

- **Licence:** Apache 2.0, "All components are available under the Apache 2.0 License on GitHub"
  (https://perses.dev/ and https://github.com/perses/perses).
- **Project status:** "Perses is a Cloud Native Computing Foundation sandbox project" (https://perses.dev/ and GitHub).
- **Release:** the perses.dev banner says "Release v0.55.0", linked to a page dated 2026/10/21. I did not confirm the
  GitHub release list.
- **Go version, conflicting sources:** GitHub README says "Go version 1.26 or greater"; the build-from-source docs
  say "GO v1.23+" (https://perses.dev/perses/docs/installation/from-source/). I report both and did not resolve it.
- **Requires Prometheus:** "You will also need a running Prometheus instance that Perses can connect to"
  (https://perses.dev/perses/docs/getting-started/). Data sources named on GitHub: Prometheus, Tempo, Loki, Pyroscope.
- **Storage for Perses' own configuration:** `database.file` (folder, yaml or json) or `database.sql` (MySQL or
  MariaDB; "PostgreSQL is not supported"). SQLite is not mentioned. The docs describe the database as the store of
  Perses configuration: provisioning "will totally override what exists in the database"
  (https://perses.dev/perses/docs/configuration/configuration/). The fetched pages do not describe Perses storing time-series data; they require a running Prometheus instance.
- **Resources and architectures:** NOT stated on the pages fetched (no RAM, no CPU, no arm64 statement).

### 5.3 Verdict for this hardware (observation)

- Neither tool stores time-series data itself. Grafana can use SQLite for its own state, but its docs call that
  unsuitable for production. Perses needs a running Prometheus. In both cases, the metrics path needs a
  Prometheus-compatible source, which myhome does not yet provide correctly (section 3).
- **Pi 3B+:** no measured RAM for either. Grafana's documented floor (512 MB) leaves little headroom beside the
  daemon. Perses's requirements are not stated. Upstream Prometheus does not publish an arm binary (section 2), so
  Prometheus on the Pi would itself need a distro package or a source build. I would not put either on the Pi
  without measurement.
- **NAS (observation only):** the NAS already runs Prometheus 2.42. A correct `/metrics` from the daemon would be
  scraped by that existing Prometheus, and Grafana or Perses could sit on the NAS too. Whether that is feasible
  depends on NAS resources, which I did not check (no live access).
- Licence: Grafana's AGPL-3.0 is a review item if it were ever modified and served over a network. I did not
  assess that.

---

## Answer in brief

- **Storage:** SQLite is workable for this workload. 40 devices x 10 series x 1/min is 4,032,000 samples per week, which measured at about 91 MiB as a `WITHOUT ROWID` table keyed `(ts, series_id)`, and about 104 MiB steady state with a 7-day rolling delete. The write rate, not the size, is the design constraint: series-major keys or a secondary index produced about 3.7-4.0 GiB per day of application writes in the same test, against about 56-72 MiB per day for the time-major key. These are workstation measurements; the Pi has not been measured.
- **Engines:** Prometheus TSDB, VictoriaMetrics and tstorage were not measured on ARM. Upstream Prometheus publishes only linux-amd64 binaries. VictoriaMetrics has no documented embedding API. tstorage is Apache-2.0 and maintained, but has no RAM figure. Recommendation on evidence: stay with SQLite, as the owner's constraint already requires.
- **Exporter:** `myhome/metrics/exporter.go` does not use `client_golang`, and concatenates per-device payloads. Each device's HELP/TYPE block repeats, which `promtool` rejects (a parse error on line 9 of a two-device test). Local Prometheus 2.53.5 accepted it anyway, but not tested on 2.42. Offline devices are served indefinitely with their last values, with no per-device `up`.
- **Charts:** uPlot (MIT, 22 KB gzip, canvas, native time axis) is the recommendation. Its latest npm release is 2025-03-14, which is the main caveat. Dygraphs (MIT, published 2026-07-27, 37 KB gzip) is the alternative.
- **Extension:** Grafana is AGPL-3.0 with a documented 512 MB evaluation floor and no Pi/arm64 statement; Perses is Apache-2.0 and a CNCF sandbox project, needs a running Prometheus, and states no resource figures. Neither is verified on a Pi 3B+.

## What could not be established

- **SD-card endurance and write amplification.** No primary source. Lifetime figures are not given here.
- **Pi 3B+ measurements** of any engine: RAM, CPU, insert latency, card writes. Section 1.5 lists what to measure.
- **Prometheus 2.42 behaviour** on the duplicate-HELP payload (only 2.53.5 was tested).
- **Prometheus RAM per series** from an official source. The only figure found (~3 KB per series) is a vendor blog.
- **VictoriaMetrics RAM and disk per sample**, and whether arm binaries are published. The docs gave no figure.
- **tstorage** bytes per point, retention behaviour and production readiness. Not in the fetched pages.
- **Grafana** arm64 support and Pi RAM. **Perses** resource requirements and arm64 support.
- **Client-side performance** of any chart library. Only author or doc claims were read; nothing was run in a browser.
- **sector unit** of `/proc/diskstats` (the fetched kernel doc does not state it).
- **Live exporter behaviour** against the real fleet, and node-exporter's port 9100 on the NAS (user-stated; not checked).
- **InfluxDB, TimescaleDB and other engines** were not researched.
- **Batching beyond 1 minute** and its effect on pages written; not measured.
- **Go version conflict for Perses** (1.26+ in the GitHub README, 1.23+ in the from-source docs). Both are quoted; not resolved.

## When I looked, and what I read

- Date: 2026-11-01 (UTC session date). All web fetches and npm/GitHub API reads were done that day.
- Local repository: worktree `feature+observability`, HEAD `33be2480`. Files read: `myhome/metrics/exporter.go`, `myhome/daemon/daemon.go` (exporter wiring), `myhome/daemon/run.go`, `myhome/events/storage.go`, `myhome/events/service.go`, `myhome/ctl/options/options.go`, `myhome/ctl/shelly/script/prometheus.go`, `internal/shelly/scripts/prometheus-metrics.js`, `internal/shelly/scripts/watchdog.js`, `internal/myhome/ui/static/main.go`, `docs/prometheus-mqtt-exporter.md`, `docs/EMBED-DB-PLAN.md`, `LICENSE`.
- Module cache (read-only): `modernc.org/sqlite@v1.50.0`, `prometheus/common@v0.70.0`.
- Local tools: `promtool` and `prometheus` 2.53.5; Python 3 stdlib `sqlite3` 3.46.1.
- Primary web sources and the version each was read at: SQLite WAL, WITHOUT ROWID and PRAGMA pages (page text as fetched 2026-11-01); Prometheus storage and exposition-format pages (2026-11-01; storage page's footer shows 2014-2026, no modified date); Prometheus download page (3.15.1, 2026-10-21); VictoriaMetrics single-node docs and GitHub (v1.155.0, 2026-10-23); Grafana docs and LICENSE (undated); Perses site and docs (v0.55.0 banner, 2026/10/21 link); npm registry metadata for uplot 1.6.32, chart.js 4.6.1, echarts 6.1.0, dygraphs 2.2.2; GitHub API for tstorage (last push 2026-03-16).
- Throwaway scratch files (not part of the repository): `sqlite_measure.py`, `sqlite_retention.py`, `prom-test.yml`, `one-device.prom`, `two-devices-concat.prom`, and downloaded npm tarballs under `pkgs/`, all under `/tmp/claude-1004/-home-fix-GIT-home-automation/39763477-6882-4b9f-922f-86bfaa818ace/scratchpad/`.
- No live system was touched. The one local Prometheus instance and HTTP server were started on 127.0.0.1 for the scrape test and stopped afterwards.

## Related, not asked

- `watchdog.js` header comment (lines 1-5) describes metrics that live in `prometheus-metrics.js`. Stale comment.
- `docs/prometheus-mqtt-exporter.md` lists `shelly_switch_activated_total` and similar names that are on the MQTT path only, not in `/metrics`.
- The `--enable-metrics-exporter` help text says "auto-enabled with device manager" but the default is `false`.
- The project LICENSE is MPL-2.0. Any future chart library needs its licence recorded alongside its pinned asset.
