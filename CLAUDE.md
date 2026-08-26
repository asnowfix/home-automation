# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Context

Hobby project with three goals: learn Go, learn Claude Code, automate the house with [Shelly devices](https://www.shelly.com/). Prefer idiomatic, educational Go patterns. Keep changes small and well-explained. Simplicity over generality. Detailed guidelines (Shelly JS, RPC patterns, config management) are in `AGENTS.md`.

## Commands

```bash
make build          # build everything (runs go generate first — required before bare `go build`)
make generate       # fetch/embed static JS/CSS assets (alpine.min.js, htmx.min.js, bulma.min.css); must run before `go build` in a fresh worktree
make test           # canonical: tests root module + all go.work sub-modules
make run            # build and run daemon locally (myhome/Makefile just does `go run .` with no args — prints help, does NOT start the daemon)
make tidy           # tidy all workspace modules

# Run the full daemon locally against the real home MQTT broker (needed to test UI/RPC changes
# against live devices, e.g. via a browser at http://127.0.0.1:6080). Run from the worktree
# whose code you want to test — each worktree's `go run` builds its own checked-out sources.
go run ./myhome daemon run --instance local --mqtt-broker tcp://192.168.1.2:1883

go test ./internal/myhome/...                    # single package
go test -v -run TestName ./path/to/package       # specific test
go test -race ./...                              # with race detector

# NOTE: pass a BARE FILENAME relative to the current directory. An absolute path fails with a
# misleading "file does not exist" even when the file is plainly there.
go run ./myhome ctl shelly script upload <device> <script.js> --no-minify
go run ./myhome ctl shelly script update <device>
go run ./myhome ctl shelly script debug <device> true

# developer tools (run from repo root)
go run ./tools/classify-events [events-dir] [testdata-dir]   # classify raw event dumps → pkg/shelly/mqtt/testdata/
```

To query live devices, use the built-in MCP server (`shelly_list`, `shelly_call` tools). It is pre-configured in `.mcp.json` with MQTT broker `tcp://192.168.1.2:1883` and approved via `enabledMcpjsonServers` in `.claude/settings.json`. The `.mcp.json` command automatically runs `go generate ./internal/myhome/ui/...` on first use in a fresh worktree (fetches CSS/JS assets required to compile); this needs internet access once per worktree. Restart Claude Code to activate.

`make test` is canonical — never bare `go test ./...` (it skips workspace sub-modules). New CI test commands must also invoke `make test`, not go directly to `go test`.

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

**`go generate` sub-module gap**: `go generate ./...` from the workspace root does NOT recurse into Go workspace sub-modules. Adding a new `//go:generate` directive to any package under `myhome/ctl/` requires registering it explicitly in **all four** of these places, or CI will fail with "undefined: DefaultXxx" compile errors:
1. Root `Makefile` — `generate` target (already has explicit lines for `pool` and `garden`)
2. `.goreleaser.yml` — `before.hooks` list
3. `.github/workflows/package-release.yml` — Windows MSI "Run go generate" step
4. Any other workflow that builds the binary directly (check for bare `go build` calls)

Workflows that validate the binary must run `make build` from the **repo root** (not `cd myhome && make build`) — the sub-Makefile has no `generate` target, so embedded assets and generated constants will be missing. The binary then lives at `./myhome/myhome`, not `./myhome`.

Gitignored generated files (`garden_defaults_generated.go`, `pool_defaults_generated.go`) are invisible to CI. Every build path must explicitly generate them; a missing call produces a silent build failure, not a lint warning.

When asked to run `myhome <args>`, use `go run ./myhome <args>` — do not rely on a pre-built binary.

## Architecture

### Go Workspace

~45 sub-modules in `go.work`, all tested by `make test`. When adding a new sub-module: `go work use <dir>`.

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

All methods share one MQTT topic (`myhome/rpc`). Adding a method requires four steps in order:
1. Add `Verb` constant → `internal/myhome/const.go`
2. Add request/response types → `internal/myhome/<service>.go`
3. Add to `signatures` map → `internal/myhome/methods.go`
4. Register via `myhome.RegisterMethodHandler()` — never create a separate MQTT subscription

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

### Merging: "LGTM" or "/merge" means merge it

The maintainer **cannot approve these pull requests**. An agent working on their behalf opens PRs
under their own account, and GitHub does not allow self-approval — so the review-approval route does
not exist here, and waiting for it deadlocks.

**When the maintainer says "LGTM" or "/merge" — in a PR comment or in conversation — that is the
instruction to merge that PR.** It carries authorisation to clear whatever is blocking it, including
using `--admin` to pass the `merge-policy` size gate, which is otherwise on the ask-every-time list.

Two things it does **not** waive, because they are about the change being correct rather than about
permission:

- **A red build still blocks.** "LGTM" approves the change, not a failing CI run. Fix the failure or
  say why it is unrelated, then merge.
- **Hardware verification still applies** where this repo requires it. A green suite is not evidence
  that a device script runs — see the emulator gaps in #496 and the three defects that passed CI and
  failed on hardware in August 2026.

Why this is written down: #551 sat blocked for a day on a size gate while its author waited for an
approval that could never arrive.

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
- **RPC handler tests**: tests that call `myhome.RegisterMethodHandler()` must restore state in `t.Cleanup` and must not call `t.Parallel()` (shared package-level map).
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
