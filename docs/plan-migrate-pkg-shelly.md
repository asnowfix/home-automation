# Plan: Migrate pkg/shelly/ to standalone go-shellies repository

## Goal

Extract the generic Shelly device library (`pkg/shelly/` and `pkg/devices/`) from
home-automation into a standalone repository at `github.com/asnowfix/go-shellies`.
Also move the MCP server (`myhome/ctl/mcp/`) and the direct-call CLI command
(`myhome/ctl/shelly/call/`) since they are generic Shelly tooling.

## Phase 1: Create plan document
- [x] Write this plan file
- [x] Commit

## Phase 2: Set up go-shellies (target repo) -- DONE

Branch: `feature/initial-library` in `/Users/fkowalski/GIT/go-shellies`

1. [x] `go mod init github.com/asnowfix/go-shellies`
2. [x] Copy `pkg/devices/` -> `devices/` (3 files: types.go, list.go, lookup.go)
3. [x] Copy `pkg/shelly/` root files -> root (device.go, config.go, registrar.go, ops.go, mdns.go, shelly.go)
4. [x] Copy all sub-package directories
5. [x] Delete 12 go.mod/go.sum files being consolidated. Keep script/go.mod and gen1/go.mod.
6. [x] Update ALL import paths
7. [x] Fix Blocker 1: remove dead `internal/myhome/net` import from device.go
8. [x] Fix Blocker 2: changed Init() to accept `extras ...InitExtra` callbacks instead of importing script directly
9. [x] Fix script/ops.go: use passed logr.Logger instead of hlog
10. [x] Remove deprecated `golang.org/x/exp/rand` from mqtt/ops.go
11. [x] Create `DeviceDiscovery` interface + `ZeroConfDiscovery` implementation
12. [x] Copy and adapt MCP server (mcp/ sub-package)
13. [x] Copy and adapt call command (call/ sub-package)
14. [x] Run `go mod tidy` for root, script/, and gen1/ modules
15. [x] Verify `go build ./...` and `go test ./...` succeed for all 3 modules
16. [x] Commit

## Phase 3: Update home-automation (this worktree) -- DONE

Branch: `feature/migrate-pkg-shelly-in-its-own-repo`

1. [x] Update go.work: remove all `./pkg/shelly/*` and `./pkg/devices` entries, add replace directives
2. [x] Update ALL import paths throughout home-automation (~90 files)
3. [x] `git rm -r pkg/shelly/` and `git rm -r pkg/devices/`
4. [x] `git rm -r myhome/ctl/mcp/` and `git rm -r myhome/ctl/shelly/call/`
5. [x] Add `replace` directives in go.work for local dev
6. [x] Update `Init()` call sites to pass `script.Init` via extras callback
7. [-] Daemon-based DeviceDiscovery: deferred (myhome.Foreach still works for now)
8. [x] Update go.mod files to require go-shellies
9. [x] Verify `make build` and `make test` succeed
10. [x] Commit

## Phase 4: Update plan document -- DONE
- [x] Mark all phases done
- [x] Commit

## Module consolidation (13 -> 3 modules)

**Root module** (`github.com/asnowfix/go-shellies`):
- Sub-packages (no individual go.mod): ble, blu, devices, ethernet, input, kvs, matter, mqtt, ratelimit, schedule, shelly, shttp, sswitch, system, temperature, types, wifi
- Deps: logr, zeroconf, cobra, mcp-go

**script module** (`github.com/asnowfix/go-shellies/script`):
- Heavy deps: goja, minify

**gen1 module** (`github.com/asnowfix/go-shellies/gen1`):
- Heavy deps: gorilla/schema

## Blockers resolved

| # | File | Issue | Fix |
|---|------|-------|-----|
| 1 | device.go:7 | Dead import `internal/myhome/net` | Delete import line |
| 2 | ops.go:21 | Active import `internal/shelly/scripts` | Add `scriptsFS fs.FS` param to Init(), caller passes `scripts.GetFS()` |
| 3 | device.go, mdns.go, shelly.go | Import `pkg/devices` | Move `pkg/devices` into go-shellies as `devices/` sub-package |

## Import path mapping

| Old | New |
|-----|-----|
| `home-automation/pkg/shelly` | `go-shellies` (package `shelly`) |
| `home-automation/pkg/shelly/<sub>` | `go-shellies/<sub>` |
| `home-automation/pkg/devices` | `go-shellies/devices` |
