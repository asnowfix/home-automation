# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Context

Hobby project with three goals: learn Go, learn Claude Code, automate the house with [Shelly devices](https://www.shelly.com/). Prefer idiomatic, educational Go patterns. Keep changes small and well-explained. Simplicity over generality. Detailed guidelines (Shelly JS, RPC patterns, config management) are in `AGENTS.md`.

## Commands

```bash
make build          # build everything (runs go generate first — required before bare `go build`)
make generate       # fetch/embed static JS/CSS assets (alpine.min.js, htmx.min.js, bulma.min.css); must run before `go build` in a fresh worktree
make test           # canonical: tests the root module + the 3 library modules (pkg/shelly, pkg/sfr, pkg/beem)
make run            # build and run daemon locally (myhome/Makefile just does `go run .` with no args — prints help, does NOT start the daemon)
make tidy           # tidy all 4 workspace modules
make check-boundaries # fail if pkg/shelly, pkg/sfr or pkg/beem import the root module

# Run the full daemon locally against the real home MQTT broker (needed to test UI/RPC changes
# against live devices, e.g. via a browser at http://127.0.0.1:6080). Run from the worktree
# whose code you want to test — each worktree's `go run` builds its own checked-out sources.
go run ./myhome daemon run --instance local --mqtt-broker tcp://192.168.1.2:1883

go test ./internal/myhome/...                    # single package
go test -v -run TestName ./path/to/package       # specific test
go test -race ./...                              # with race detector

# NOTE: <device> addressed by ".local" name or IP resolves directly, no daemon needed
# (mDNS / net.ParseIP in LookupDevices()). A bare name (no ".local", not an IP) needs a
# daemon reachable on --instance to resolve via the device.lookup RPC — with none running,
# ctl fails after a multi-second timeout. Prefer "<device>.local" or its IP. See issue #599.

# NOTE: pass a BARE FILENAME relative to the current directory. An absolute path fails with a
# misleading "file does not exist" even when the file is plainly there.
go run ./myhome ctl shelly script upload <device> <script.js> --no-minify
go run ./myhome ctl shelly script update <device>
go run ./myhome ctl shelly script debug <device> true

# developer tools (run from repo root)
go run ./tools/classify-events [events-dir] [testdata-dir]   # classify raw event dumps → pkg/shelly/mqtt/testdata/
```

To query live devices, use the built-in MCP server (`shelly_list`, `shelly_call` tools). It is pre-configured in `.mcp.json` with MQTT broker `tcp://192.168.1.2:1883` and approved via `enabledMcpjsonServers` in `.claude/settings.json`. The `.mcp.json` command automatically runs `go generate ./internal/myhome/ui/...` on first use in a fresh worktree (fetches CSS/JS assets required to compile); this needs internet access once per worktree. Restart Claude Code to activate.

`make test` is canonical — bare `go test ./...` from the repo root now covers the entire root module (everything except pkg/shelly, pkg/sfr, pkg/beem — see "Go Workspace" below), but still misses those 3 library modules, so `make test` (or the equivalent per-module loop) is still required for full coverage. New CI test commands must also invoke `make test`, not go directly to `go test`.

### Test timeout — derived from measurement, never guessed

`TESTFLAGS` in the root `Makefile` carries an explicit `-timeout`. **Whenever a full `make test`
succeeds, set the timeout to the slowest package's measured duration plus 10%** (`N + N×0.1`), and
record the measurement in the comment above `TESTFLAGS` with its date.

The timeout then tracks what the suite actually costs instead of drifting until it silently fails.

Two things to get right:

- **Go applies `-timeout` per package** (per test binary), not to the whole run. So `N` is the
  **slowest package**, currently `internal/shelly/scripts`, not `make test`'s overall wall clock.
- **Apply it to every target that runs the suite**, and keep it in its own variable
  (`TESTTIMEOUT`), not folded into `TESTFLAGS`. CI calls `make cover` five times across workflows
  against `make test` twice, and `test-race` overrides `TESTFLAGS` — a timeout folded into the flags
  is silently dropped by both. That is how #552 survived its own first fix.
- **Measure under the SLOWEST configuration, which is `-race` — not `cover`.** Same-job evidence
  2026-08-27 (GitHub Actions run `33041300536`, job `98417041803`) measured `cover` at 583.150s (PR
  branch) and 583.390s (base `main`), both *faster* than 592.565s under `-race`. The #556 failure
  that looked like the opposite — `cover` timing out at exactly 600.016s — was Go's default 600s
  `-timeout` ceiling firing before `-timeout` was wired into `cover`, not a real measurement of its
  cost; it is wired in today (`Makefile`'s `cover` target). CI calls `make cover` five times across
  workflows against `make test` twice, so the measurement must still be taken under whichever
  configuration is currently slowest, which today is `-race`.
- **Only a successful run may set it.** A run that timed out tells you nothing about how long the
  suite needs; raising the timeout from a failed run's duration just guesses again, more slowly.

Why this rule exists: that package grew 507s → 591s → 627s in three days and crossed Go's **600s
default** on 2026-08-25, after which `make test` could not pass for anyone. Two runs failed at
600.654s and 600.651s — agreeing to within 3ms — which is what finally identified a fixed limit
rather than the CPU contention everyone had assumed (#552).

**When two runs fail at the same duration to within milliseconds, suspect a limit, not load.**

#### The suite now outlives a foreground command — background it or lose the agent

There are **two independent 600-second ceilings**, and this suite crossed both at once:

| limit | value | what it kills |
|---|---|---|
| Go's default `-timeout` | 600 s | the test run itself (#552) |
| the agent harness's max foreground `Bash` timeout | 600 s | **the agent waiting on it** |

Raising the Go timeout fixes the first and makes the second *worse*: a foreground `make test` now
runs long enough to guarantee the caller is killed first, mid-run, leaving uncommitted work and no
report.

So **never run the suite in the foreground** — not merely because the log floods context, which was
the original reason, but because **the run outlives the command**. Background it to a file with
`run_in_background`, then poll that file in **separate, short** `Bash` calls within the same turn: a
single poll loop is itself a foreground command and dies at the same 600 s mark. (Observed
2026-08-26: a poll loop was killed at exactly 10m00s while waiting on CI.)

This is also the strongest argument that raising the timeout is a stopgap. The real fix — splitting
the package or sharing emulator state (#552) — is what brings the suite back under the ceilings
rather than negotiating with one of them.

**`go generate` sub-module gap (mostly resolved by #359)**: before the module collapse, `go generate ./...` from the workspace root did not recurse into any of the ~59 separate Go workspace modules, so every `//go:generate` directive anywhere in the tree needed registering in 4 separate places. Now that only `pkg/shelly`, `pkg/sfr` and `pkg/beem` remain as separate modules, this gap is gone for anything in the root module: a `//go:generate` directive added to any root-module package (which is now almost everything — `myhome/*`, `internal/*`, `hlog`, `pkg/devices`, `pkg/tapo`, `pkg/version`) is picked up by the single `go generate ./...` call in `make generate`, `.goreleaser.yml`, and `package-release.yml`'s Windows step — no extra registration needed.

The gap still applies **only** if you add a `//go:generate` directive inside `pkg/shelly`, `pkg/sfr` or `pkg/beem` and expect the root build to pick it up automatically — it won't, since those are separate modules; run `go generate` from inside that module explicitly (and register it in the same 4 places if the root build's `make build`/`.goreleaser.yml`/`package-release.yml` need its output).

Workflows that validate the binary must run `make build` from the **repo root** (not `cd myhome && make build`) — the sub-Makefile has no `generate` target, so embedded assets and generated constants will be missing. The binary then lives at `./myhome/myhome`, not `./myhome`.

Gitignored generated files (`garden_defaults_generated.go`, `pool_defaults_generated.go`) are invisible to CI. Every build path must explicitly generate them; a missing call produces a silent build failure, not a lint warning.

When asked to run `myhome <args>`, use `go run ./myhome <args>` — do not rely on a pre-built binary.

## Architecture

### Go Workspace

4 modules in `go.work` (collapsed from ~59 in #359): the root app module, plus 3 standalone library modules that must stay independently importable and must never import the root module (enforced by `make check-boundaries`):
- `pkg/shelly` — Shelly Gen2+ RPC client (MQTT + HTTP); see `docs/EXTRACT-PKG-SHELLY-PLAN.md` for its longer-term extraction to its own repository.
- `pkg/sfr` — SFR box (French ISP router) local API client.
- `pkg/beem` — Beem Energy API client.

Everything else (`myhome/*`, `internal/*`, `hlog`, `pkg/devices`, `pkg/tapo`, `pkg/version`, `tools/*`) lives in the root module. Don't create a new sub-module for a new package — add it to the root module instead; only `pkg/shelly`, `pkg/sfr` and `pkg/beem` warrant the standalone-module overhead (external reusability with zero app coupling).

### Binary

`myhome/` builds the single `myhome` binary:
- `myhome run` — daemon (eager MQTT connect, receives retained messages at startup)
- `myhome ctl ...` — device control CLI (lazy MQTT connect, auto-connects on first RPC)

### Three-Tier Layer Rule

```
myhome/ctl/shelly/       ← CLI only: cobra commands, fmt.Printf output, flag parsing
internal/myhome/shelly/  ← MyHome business logic: workflows, version tracking, policies
pkg/shelly/              ← generic Shelly API: direct RPC calls, script ops, MQTT/HTTP channels
```

No business logic in `myhome/ctl/`. No MyHome-specific code in `pkg/shelly/`. Utilities shared across CLI packages go in `internal/myhome/`, not `myhome/ctl/` (causes import cycles).

### RPC System

All methods share one MQTT topic (`myhome/rpc`). Adding a method requires two steps in order:
1. Add `Verb` constant → `internal/myhome/const.go`
2. Add request/response types (if new) → `internal/myhome/<service>.go`, then register the handler via `myhome.Register(verb, handler)` — never create a separate MQTT subscription

`Register[P, R any](verb Verb, h func(ctx context.Context, p P) (R, error))` (`internal/myhome/methods.go`) replaces the old `signatures` map + `RegisterMethodHandler` pair: P and R are normally inferred from `h`'s own signature, so most registrations are a one-line call passing a method reference directly, e.g. `myhome.Register(myhome.HeaterGetConfig, s.HandleGetConfig)`. The wire request's `Params` (and the response's `Result`) travel as `json.RawMessage` end to end and are decoded exactly once, directly into `P` — there is no more runtime `reflect.TypeOf` check and no marshal-then-unmarshal round trip. Registering the same verb twice overwrites the previous registration (there is no `Unregister`); tests that need to swap a handler save the previous `*Method` via `myhome.Methods(verb)` and restore it with `myhome.RestoreMethod` in `t.Cleanup`.

On the caller side, `myhome.TheClient.CallE(ctx, verb, params)` stays untyped (it must satisfy the `Client` interface across the wire client, `FakeClient`, and `DeviceManager`'s in-process short-circuit alike) — use the generic wrapper instead: `result, err := myhome.Call[ParamsType, ResultType](ctx, myhome.TheClient, verb, params)`. For a handler with no meaningful params or result, use `any` for that type parameter (e.g. `myhome.Call[any, *myhome.PoolGetStatusResult](ctx, myhome.TheClient, myhome.PoolGetStatus, nil)`).

### Key Packages

| Package | Role |
|---|---|
| `internal/myhome/` | RPC types, verb registry, MQTT RPC server |
| `myhome/daemon/` | Startup wiring: MQTT client, device manager, services |
| `myhome/devices/impl/` | Device discovery and management |
| `myhome/mqtt/` | MQTT client + `RecordingMockClient` for tests |
| `myhome/temperature/` | Temperature service (SQLite, setpoints, forecasts) |
| `myhome/occupancy/` | Occupancy detection via LAN presence checks |
| `pkg/shelly/script/` | JS upload, minification, KVS version tracking |
| `hlog/` | Custom logger — `hlog.GetLogger("pkg/name")` |

Ports: 6080 (dev web UI), 80 (systemd), 6060 (pprof), 9100 (Prometheus).

## Conventions

### GitHub Issues

A **self-contained issue** has full context and does not depend on any coding agent's or human's
memory of a prior conversation or session — anyone (agent or human) can pick it up cold, with no
other source of information than what the issue itself contains. It may, and should, reference
external sources (docs URLs) and/or other issue(s) and/or PR(s), but must not assume the reader
was present for the discussion that led to filing it.

**Every issue filed in this repo must be self-contained** per the above definition — this applies
whether a human or a coding agent is the one filing it. Before creating an issue, write it as if
handing it to a coding agent with a cold context window: no "as discussed above," no assuming the
reader saw the same live-debugging session or chat history that motivated the issue. Cross-issue
dependencies must be made explicit (e.g. "blocked by #123," "must land after #456") rather than
implied by filing order or narrative context.

**Close an issue only on empirical evidence, and put that evidence in the closing comment.** A fix
that is present in the source is *not* verified — run the thing. For a data race, that means the
race detector's output on the affected package; for a device bug, a live measurement; for a crash,
a regression test that fails without the fix. An issue whose fix has been confirmed only by reading
the diff stays **open**, with a note saying so.

Grep alone is not evidence. Verifying a claim against `origin/main` while sitting on a feature
branch — mixing `git grep <rev>` with working-tree greps — silently produces contradictory answers.
Pick one and say which.

**Keep the umbrella issue of a long campaign current.** For multi-week work, the top-level issue is
the status board: post an update whenever a gate opens or closes, a measurement lands, or a release
decision changes, using explicit state words (new / pending / implementing / verifying / discarded /
closed-by-PR / blocked). Do not post when nothing changed.

### `events.db` is the long-term record — query it before asserting a rate

The daemon persists every device event to a SQLite database, so **how often something actually
happened is a query, not a guess** — weeks or months after the fact, with no need to have been
watching at the time. Treat it as the first place to look whenever a claim depends on a frequency,
a duration, or "does this ever happen?".

**Where it lives.** `events.db`, a plain relative filename by default, next to `myhome.db`.
Overridable with `--events-db`, `MYHOME_EVENTS_DB`, or the `events.db` config key; `ctl events` falls
back to `~/.myhome/events.db`.

⚠️ **Retention is 90 days by default, and the daemon enforces it hourly** (`myhome/events/service.go`
tickers once an hour and purges everything older than `--events-retention`). It is long-term, not
permanent. **Anything you may want beyond that window must be snapshotted before it ages out** — copy
the file; there is no archive.

**Schema** — one `events` table: `ts`, `received_at`, `device_id`, `component`, `event`, `severity`,
`data` (JSON), with indexes on `ts`, `device_id`, `event` and `severity`, so filtering by any of those
is cheap. `sensor_daily_stats` holds pre-aggregated per-day sensor min/max/sum.

**How to query.** `myhome ctl events list` and `... follow` for browsing. For anything analytical, go
at the file with `sqlite3` directly — aggregation is the whole point:

```sql
-- does this event pair up, or are edges being lost? group by day against a restart proxy
SELECT date(ts,'unixepoch','localtime') d,
       SUM(event='config_changed')              AS cfg,
       SUM(event='pool.water_supply_protected') AS prot,
       SUM(event='pool.water_supply_restored')  AS rest
FROM events WHERE device_id='shellypro1-ec62608c0230'
GROUP BY d HAVING prot>0 OR rest>0 ORDER BY d;
```

**Worked example, #523 (2026-08-27).** An issue claimed a safety fact could go stale "indefinitely"
because an input edge might be missed, and a fix was designed, briefed, implemented, reviewed and
nearly merged on that premise. One query over 71 days settled it: on **every** day without
configuration activity the edges paired **1:1 with zero exceptions**, and every unpaired event fell on
a day with restarts, where init legitimately re-asserts state. The same query showed the real cadence
— one episode per day lasting 15–25 minutes — and pinned the single observation the issue was built
on to **92 seconds into an ordinary episode**, i.e. the fact was correct and the cross-check was the
anomaly. The issue closed as *not a defect*; the code was deleted.

**The rule that follows:** a change proposing to mitigate a failure must state **the observed rate of
that failure**, or say "never observed" explicitly. Consequence alone justifies nothing on a device
with ~7 KB of free heap. If the failure would emit an event, that rate is already in `events.db` and
costs one query — and "nobody looked" is how unnecessary code reaches review.

### Live devices: capture before-state, and never trust a command's name for its blast radius

**A provisioning command's name, `--help`, and the issue that queues it all understate what it
writes.** Read its implementation before running it against a device in service. Worked example,
2026-09-02 (#589): the queued task was "re-provision to verify #528", and reading the code first
found that `ctl pool update` **skips schedules entirely on a Pro1** (gated on `pro3`), that
`ctl pool add` rewrites **24 KVS keys** rather than just schedules, and that `pool add`'s own help
text contradicts its code. A fourth defect appeared only at runtime: the speed lookup reads from
*another* pool device, that device timed out, and the code **fell through to a hardcoded default**,
silently rewriting the running pump's speed from `max` to `eco`. A guard written specifically to
prevent a silent reset failed open.

The discipline that made this recoverable:

- **Capture the complete before-state to a file first** — `Schedule.List` *and* the full KVS map
  (`KVS.GetMany`, which truncates at ~22 items over MQTT, so page it). "I'll put it back" means
  nothing without it.
- **Shelly etags are content-derived, so they prove restoration.** After restoring a value, compare
  its etag to the one captured before: a match is proof of *exact* restoration, not merely an
  equivalent-looking value. This is the cheapest verification available and nothing else gives it.
- **Diff before/after programmatically**, and report which fields are identical rather than eyeballing
  a listing.
- **Re-read the facts a live experiment mutated** on the next session rather than trusting the comment
  that says they were restored. One read-only RPC; the failure it guards against is silent.

### NAS operational facts

- **`myhome.service` restarts every 6 hours by design** — the packaged unit sets
  `RuntimeMaxSec=21600`. So `NRestarts` climbing is expected, not a crash loop; check
  `ExecMainStatus`/`Result` before treating it as one. Each restart triggers a rediscovery burst of
  ~41 `device.online` events, roughly **164/day**, which is a large share of what #499 investigates.
- **The journal is `Storage=volatile`** and the daemon is chatty enough to rotate it away within
  ~15 minutes. **NAS post-mortem evidence does not survive** — use `events.db` (see above), which does.
- **`sqlite3` is not installed on the NAS.** Query `events.db` with `python3`'s `sqlite3` module
  against `file:/var/lib/myhome/events.db?mode=ro` instead.
- **`myhome-update.timer`** installs whatever `releases/latest` resolves to, nightly (00:00), so a
  hand-installed binary or a draft release is overwritten — with a *downgrade* if `releases/latest`
  is older (measured 2026-10-20: a hand-built 0.12.4-lux1 was replaced by 0.12.4 at midnight). To
  keep a different build on the NAS, disable (or mask) the timer first and re-enable it once that
  build is the published latest.
- **`dpkg -i` runs the package's postinst, and that is not just a binary swap.** `update.sh` itself
  only fetches the `.deb` and runs `dpkg -i`, but the postinst then (a) runs
  `myhome ctl shelly script update '*'`, pushing the packaged scripts to **every known device that
  already has the script loaded** whose KVS version marker (`script/<name>` = sha1 of the packaged
  source) differs — that includes the production pool pump if it carries a hand-deployed build;
  (b) `systemctl reenable`s **both** timers, so a merely *disabled* update timer comes back (a
  *masked* one makes postinst fail, see below); (c) prompts for credentials and rewrites
  `/var/lib/myhome/.env` when stdin is a tty and `DEBIAN_FRONTEND` is not `noninteractive`. There is
  no dry-run; to preview (a), compare each device's `script/<name>` KVS value with `sha1sum` of the
  packaged `internal/shelly/scripts/<name>`.
- Pool provisioning (`Setup`/`AddDevice`/`UpdateDevice`) is reachable **only** from `myhome/ctl/pool/`,
  never from the daemon; the daemon's auto-setup applies solely to devices lacking `watchdog.js` as
  script #1. A `dpkg` exit code of 1 with state `iF` is expected while the timer is masked: postinst
  tries to enable it, systemd refuses, everything before that has already succeeded.

### Sub-agents

**Every sub-agent must persist its progress outside its own context**, incrementally, as it works —
not only in its final report. A sub-agent can die mid-task (API error, spend limit, stall) and its
worktree may be auto-removed if it never committed, taking every finding with it.

When launching a sub-agent, give it exactly one of these and say which:

- **A progress file** at a path you define (e.g. `docs/<issue>-progress.md`, or a scratchpad path),
  appended after each meaningful step: what was tried, what was measured, what was ruled out.
- **Comments on the GitHub issue** it was given as its objective — preferred when the work is tied
  to an issue, since the findings then survive for whoever picks it up next and satisfy the
  self-contained-issue rule above.

Live-device measurements (`mem_peak`, RPC responses, event-DB queries) and negative results must be
written down as they are obtained. Re-deriving them means re-touching real hardware.

Sub-agents must not `git push` or open PRs — the coordinator does that from the sub-agent's worktree.

#### Running tests in a sub-agent

**Commit first, then run tests.** A sub-agent that dies mid-run loses everything uncommitted, and its
worktree may be auto-removed.

**Never run the test suite in the foreground.** Go test output under `-race`, with the script
emulator's per-call logging, is large enough to consume a sub-agent's context before it can act on
the result.

**Never end a turn in order to wait for a test run.** This is the #432 stall mode. Ending a turn
terminates the sub-agent, and nothing re-invokes it when its background job finishes — so an agent
that signs off with "waiting for the test run, will resume when notified" is simply gone.

Measured directly on 2026-08-11 with a throwaway agent that backgrounded a 200-second job and then
ended its turn:

- the agent stopped after **9.8 s**, reported by the harness as `completed`;
- its child kept running and finished normally **3.5 minutes later**;
- the agent received **no** automatic notification of that completion — confirmed by asking the
  agent itself after resuming it;
- the agent's own completion notification fired **immediately**, while its child was still alive.

Two consequences worth acting on:

- **The child's work is not lost, only orphaned.** Before re-running anything for a stalled agent,
  look for its output on disk — a completed `make test` costs ~6 minutes to repeat for nothing.
- **A stalled agent can be resumed** by the coordinator with `SendMessage`, picking up its
  transcript. That is a recovery, not the design; the agent must not plan around it.

Do not treat "the agent's notification arrived" as meaning its background work has finished — the
observed behaviour above shows the two are unrelated.

**And do not treat a *missing* notification as meaning the agent died.** The inverse error is just as
costly and easier to make, because the stall mode above primes you to expect it.

Observed 2026-09-03: an agent's progress file ended at *"Next: full `make test`"* and no completion
notification had arrived after ~14 hours of wall clock, so it was declared stalled. It had not
stalled — the coordinating session had itself been suspended repeatedly, inflating the agent's
apparent elapsed time. The agent was still running, finished normally with a **green full suite**, and
held a **commit that was never pushed** because the coordinator had already "recovered" its work. A
pull request was opened stating, as fact, that the agent died mid-suite and that CI would be the first
verification. Both were false, and the error was caught only because the agent flagged the discrepancy
itself in its final report.

**An agent that is busy and an agent that is dead are both silent.** So do not infer either from
silence — look at what the work itself shows, in this order, before concluding anything:

1. `git log` on its branch, and whether the branch head has moved since you last looked;
2. its worktree (`git -C <worktree> status`);
3. its progress file's modification time, not just its contents — a file whose last line reads
   "next: X" may simply mean X is in progress.

**Wall-clock duration is not a usable liveness signal when the coordinator can be suspended.** A
90-minute budget can legitimately span many hours of real time. If you need to know, check the branch.

**If you do conclude an agent is dead, say so as an inference and not as fact** — especially in a pull
request description, which outlives the session and will be read as evidence by whoever reviews it.

The required pattern is: **background the run, then poll it to completion inside the same turn.**

1. Start the suite with `run_in_background`, writing to a file.
2. Loop: check the output file periodically until the run finishes or the timeout expires.
3. Read results by **grepping the file** (`^(--- FAIL|FAIL|ok )`), never by inhaling it whole.
4. **Report the wall-clock duration of the test run** in the final report, alongside pass/fail.

The coordinator **must give an overall timeout at agent startup** — the agent stops polling and
reports what it has when that budget is exhausted, rather than looping forever. Choose it from the
measured suite duration with headroom: a full `make test` on `origin/main` took **5 min 47 s**
(346.67 s wall clock) on 2026-08-11, so ~15 minutes is a reasonable default budget. These tests are
known to slow down under CPU contention (#393), and several agents may share the machine.

Duration reporting is not bookkeeping: it is how the suite's cost stays visible, so the budget above
is re-derived from measurement rather than guessed.

### Go

- **CLI output**: `fmt.Printf()` for user-facing messages; `hlog` for internal/debug logging. Never `log.Info()` in CLI commands.
- **Config options**: adding any new option requires updating 4 files — `options.go`, `run.go`, `docs/configuration.md`, `myhome-example.yaml`. Env var pattern: `MYHOME_<SECTION>_<KEY>`.
- **RPC handler tests**: `myhome.Register(registry, verb, handler)` takes an explicit `*myhome.Registry` (constructed via `myhome.NewRegistry()`) instead of writing to a shared package-level map, so tests should build their own `Registry` per test and are free to call `t.Parallel()`. There is still no `Unregister`, so a test that needs to restore a previous registration should save the prior `*myhome.Method` (via `registry.Methods`) and put it back with `registry.RestoreMethod` in `t.Cleanup` — but only when reusing a shared `Registry` (e.g. the production one in `myhome/daemon`); a test-local `Registry` needs no cleanup at all.
- **Database migrations**: Use `COUNT(*)` (returns int) not bool when checking SQLite column existence. See AGENTS.md "Database Patterns".
- **SQLite database paths**: new databases use a plain relative filename (e.g. `"foo.db"`), matching `myhome.db`. Do not invent a new default directory (e.g. `~/.myhome/`, XDG paths) unless all existing databases already use it. If a flag or config key lets the user supply an absolute path, the `NewStorage` constructor must call `os.MkdirAll(filepath.Dir(path), 0o755)` before opening the file — SQLite cannot create missing parent directories.
- **File moves**: always `git mv`, never delete-and-recreate (preserves `git log --follow` history).
- **Non-trivial tasks**: create a plan file under `docs/` before writing code; mark each phase done before starting the next; commit plan updates alongside the implementation.
- **Resilience — internet-optional**: the system must remain fully operational on the local network when the internet is unreachable. Features that use remote sources (weather, cloud APIs, firmware checks) must time out and degrade gracefully; they must not block or break local operation. Always add a timeout and a fallback/no-op path before shipping any code that calls an external URL.
- **Resilience — daemon-optional per device**: each Shelly device must continue operating normally when the `myhome` daemon is down. Cross-device automation flows (device A triggers device B via the daemon) may pause during an outage, but no device's core function may depend solely on the daemon. Before moving logic from a device script into the daemon, explicitly document the degraded mode in the PR description.

### Shelly JavaScript

Shelly runs a modified Espruino (ES5, no hoisting, limited ES6). Violations below crash devices or cause silent failures:

- **No hoisting** — define every function before it is referenced, including callback arguments.
- **Max 2–3 levels of nested anonymous functions** — the engine crashes above this. Extract named top-level functions instead.
- **Never empty catch blocks** — `catch (e) {}` becomes `catch {}` after minification, causing a syntax error. Always reference `e`: `catch (e) { if (e && false) {} }`.
- **Property checks** — use `"prop" in obj`, not `obj.prop !== undefined` (minifier breaks the latter).
- **No `[].shift()` / `[].unshift()`** — not supported; use manual loops.
- **No `Array.prototype.slice.call(arguments)`** — may fail; iterate with a `for` loop.
- **Use `var`** (not `let`/`const`) for maximum firmware compatibility.
- **Upload with `--no-minify`** when debugging; minification is fine in production if the rules above are followed.
- **KVS keys**: lowercase, hyphens and forward slashes only — pattern `script/<name>/<key>`.
- **Per-script limits**: 5 timers, 5 event subscriptions, 5 status-change subscriptions, 5 concurrent RPC calls, 10 MQTT subscriptions.
- **Storage**: Use `Script.storage` for script-internal data, `KVS` for external config, in-memory vars for cache. See AGENTS.md "Data Storage Patterns".
- **Timer limits**: Use single recurring timer with task queue for sequential async ops to avoid exhausting 5-timer limit. See AGENTS.md "Resource Limit Workarounds".
- **Async state rebuild guard**: When a multi-step async chain rebuilds shared state (e.g. KVS.List → N×KVS.Get), set a `STATE.reloading` flag and have event handlers that read that state defer themselves via `queueTask` instead of silently dropping work. Clear the flag in every exit path (normal, empty-result, error). See AGENTS.md "Defer Incoming Events During Multi-Step Async State Updates".
