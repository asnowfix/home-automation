# Plan: Migrate pkg/shelly/ to standalone go-shellies repository

## Goal

Extract the generic Shelly device library (`pkg/shelly/` and `pkg/devices/`) from
home-automation into a standalone repository at `github.com/asnowfix/go-shellies`.
Also move the MCP server (`myhome/ctl/mcp/`) and the direct-call CLI command
(`myhome/ctl/shelly/call/`) since they are generic Shelly tooling.

## Phase 1: Create plan document
- [x] Write this plan file
- [ ] Commit

## Phase 2: Set up go-shellies (target repo)

Branch: `feature/initial-library` in `/Users/fkowalski/GIT/go-shellies`

1. `go mod init github.com/asnowfix/go-shellies`
2. Copy `pkg/devices/` -> `devices/` (3 files: types.go, list.go, lookup.go)
3. Copy `pkg/shelly/` root files -> root (device.go, config.go, registrar.go, ops.go, mdns.go, shelly.go)
4. Copy all sub-package directories (ble, blu, ethernet, gen1, input, kvs, matter, mqtt, ratelimit, schedule, script, shelly, shttp, sswitch, system, temperature, types, wifi)
5. Delete 12 go.mod/go.sum files being consolidated (blu, ethernet, input, kvs, mqtt, schedule, shelly, shttp, sswitch, system, types, wifi). Keep script/go.mod and gen1/go.mod.
6. Update ALL import paths: `home-automation/pkg/shelly` -> `go-shellies`, `home-automation/pkg/devices` -> `go-shellies/devices`
7. Fix Blocker 1: remove dead `internal/myhome/net` import from device.go
8. Fix Blocker 2: add `scriptsFS fs.FS` parameter to root `Init()` in ops.go, remove `internal/shelly/scripts` import
9. Fix script/ops.go: replace `hlog.GetLogger` with the logr.Logger passed as parameter (hlog is a home-automation internal package)
10. Remove deprecated `golang.org/x/exp/rand` from mqtt/ops.go (Go 1.22+ auto-seeds math/rand)
11. Create `DeviceDiscovery` interface + `ZeroConfDiscovery` implementation
12. Copy and adapt MCP server (mcp/ sub-package): accept DeviceDiscovery, remove myhome imports
13. Copy and adapt call command (call/ sub-package): accept DeviceDiscovery, remove myhome imports
14. Run `go mod tidy` for root, script/, and gen1/ modules
15. Verify `go build ./...` succeeds for all 3 modules
16. Commit

## Phase 3: Update home-automation (this worktree)

Branch: `feature/migrate-pkg-shelly-in-its-own-repo`

1. Update go.work: remove all `./pkg/shelly/*` and `./pkg/devices` entries
2. Update ALL import paths throughout home-automation to point to go-shellies
3. `git rm -r pkg/shelly/` and `git rm -r pkg/devices/`
4. `git rm -r myhome/ctl/mcp/` and `git rm -r myhome/ctl/shelly/call/` (moved to go-shellies)
5. Add `replace` directives for local dev: `github.com/asnowfix/go-shellies => /Users/fkowalski/GIT/go-shellies`
6. Update `Init()` call sites to pass `scripts.GetFS()` as the new fs.FS parameter
7. Create daemon-based `DeviceDiscovery` implementation (wraps `myhome.TheClient.LookupDevices`)
8. Run `go mod tidy` across affected modules
9. Verify `make build` and `make test` succeed
10. Commit

## Phase 4: Update plan document
- [ ] Mark all phases done
- [ ] Commit

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
