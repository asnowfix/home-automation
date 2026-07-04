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

go run ./myhome ctl shelly script upload <device> <script.js> --no-minify
go run ./myhome ctl shelly script update <device>
go run ./myhome ctl shelly script debug <device> true

# developer tools (run from repo root)
go run ./tools/classify-events [events-dir] [testdata-dir]   # classify raw event dumps → pkg/shelly/mqtt/testdata/
```

To query live devices, use the built-in MCP server (`shelly_list`, `shelly_call` tools). It is pre-configured in `.mcp.json` with MQTT broker `tcp://192.168.1.2:1883` and approved via `enabledMcpjsonServers` in `.claude/settings.json`. The `.mcp.json` command automatically runs `go generate ./internal/myhome/ui/...` on first use in a fresh worktree (fetches CSS/JS assets required to compile); this needs internet access once per worktree. Restart Claude Code to activate.

`make test` is canonical — never bare `go test ./...` (it skips workspace sub-modules). New CI test commands must also invoke `make test`, not go directly to `go test`.

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
