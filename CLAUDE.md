# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Context

Hobby project with three goals: learn Go, learn Claude Code, automate the house with [Shelly devices](https://www.shelly.com/). Prefer idiomatic, educational Go patterns. Keep changes small and well-explained. Simplicity over generality. Detailed guidelines (Shelly JS, RPC patterns, config management) are in `AGENTS.md`.

## Commands

```bash
make build          # build everything (runs go generate first)
make test           # canonical: tests root module + all go.work sub-modules
make run            # build and run daemon locally
make tidy           # tidy all workspace modules

go test ./internal/myhome/...                    # single package
go test -v -run TestName ./path/to/package       # specific test
go test -race ./...                              # with race detector

go run ./myhome ctl shelly script upload <device> <script.js> --no-minify
go run ./myhome ctl shelly script update <device>
go run ./myhome ctl shelly script debug <device> true
```

`make test` is canonical — never bare `go test ./...` (it skips workspace sub-modules). New CI test commands must also invoke `make test`, not go directly to `go test`.

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
- **File moves**: always `git mv`, never delete-and-recreate (preserves `git log --follow` history).
- **Non-trivial tasks**: create a plan file under `docs/` before writing code; mark each phase done before starting the next; commit plan updates alongside the implementation.

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
