# Plan: Migrate pkg/shelly/ to standalone go-shellies repo (v2)

## Why a v2

v1 (commits `c1423f7`, `962bce1`, `f947b68` on `feature/migrate-pkg-shelly-in-its-own-repo`) extracted `pkg/shelly/`, `pkg/devices/`, `myhome/ctl/mcp/` and `myhome/ctl/shelly/call/` into `go-shellies` by **flat-copying** the trees. Two regressions:

1. **History was lost.** The single `feat: add Shelly device library migrated from home-automation` commit on `go-shellies` has no ancestry to the original files. `git log --follow` shows nothing before the migration.
2. **Scope was too narrow.** Only `mcp/` and `shelly/call/` made it across; the rest of the generic shelly CLI tree (`kvs`, `wifi`, `mqtt`, `sys`, `status`, `reboot`, `components`, `jobs`) stayed in `home-automation`. The new repo is library-only — there's no standalone `shelly` binary.

v2 redoes the work using `git filter-repo` so history is preserved, and pulls more of `myhome/ctl/shelly/` over so `go-shellies` ships a real `shelly` binary.

## Goal

- `github.com/asnowfix/go-shellies` is a self-contained Go module that:
  - **Library**: every `pkg/shelly/*` package and `pkg/devices/`. Three Go modules: root, `script/`, `gen1/` (matches v1).
  - **Binary**: `shelly` CLI with subcommands for direct device calls plus a JS device emulator. No dependency on the myhome daemon.
- `home-automation` keeps only myhome-business CLI subcommands (`script`, `setup`, `follow`) and depends on `go-shellies` as a library for everything else.

## Scope

### Migrating to `go-shellies/cmd/shelly/`

| Subcommand | Notes |
|---|---|
| `call` | Direct RPC call to a device |
| `kvs` | get / set / delete |
| `wifi` | configure |
| `mqtt` | configure / status |
| `sys` | system info |
| `status` | device status |
| `reboot` | reboot device |
| `components` | list components |
| `jobs` | list / cancel / schedule |
| `mcp` | MCP server (stdio) |
| `emulate` | **new** — wraps `script/run.go` goja runtime to run a Shelly JS script locally against an emulated device |

### Staying in `home-automation/myhome/ctl/shelly/`

- `script` — depends on `internal/myhome/shelly/script` (heater/pool deploy logic) and `internal/shelly/scripts` (embedded myhome JS scripts).
- `setup` — depends on `internal/myhome/shelly/setup` and `internal/myhome/net`.
- `follow` — depends on `internal/myhome/shelly/script`.

These can move later, once the lib is stable and we decide how to split generic vs. myhome-specific operations.

### Decisions (locked in by user)

1. Force-push v1 branches off origin (`feature/migrate-pkg-shelly-in-its-own-repo` on home-automation, `feature/initial-library` on go-shellies). Both are unmerged, no PRs.
2. Keep go-shellies's `main` seed (LICENSE + README) as ancestor; merge the rewritten history onto it.
3. Drop the `--via daemon` relay path entirely from the standalone CLI. Direct device calls only.
4. Single binary at `cmd/shelly/` in the root module — no extra Go module for the binary.
5. `git-filter-repo` is installed.

## Layout after migration

```
go-shellies/
├── LICENSE                 (kept from seed)
├── README.md               (kept from seed; rewritten in HEAD)
├── go.mod                  (root: github.com/asnowfix/go-shellies)
├── *.go                    (was pkg/shelly/*.go: device.go, config.go, ops.go, ...)
├── ble/, blu/, kvs/, ...   (was pkg/shelly/<sub>/)
├── devices/                (was pkg/devices/)
├── script/                 (was pkg/shelly/script/, sub-module)
├── gen1/                   (was pkg/shelly/gen1/, sub-module)
└── cmd/
    └── shelly/             (was myhome/ctl/shelly/)
        ├── main.go         (cobra root)
        ├── call/, kvs/, wifi/, mqtt/, sys/, status/, reboot/, components/, jobs/
        ├── mcp/            (was myhome/ctl/mcp/)
        ├── emulate/        (new)
        ├── dispatch/       (new — replaces internal/myhome.Foreach)
        └── options/        (new — minimal CLI flags, no myhome config)
```

## Phases

### Phase 1: Plan + rollback v1 — DONE when committed
- [x] Write this plan
- [x] Commit plan on the worktree branch
- [x] `git push origin --delete feature/migrate-pkg-shelly-in-its-own-repo` (home-automation)
- [x] `git -C /Users/fix/Desktop/GIT/go-shellies push origin --delete feature/initial-library`

### Phase 2: filter-repo extraction
- [x] Single-branch clone of home-automation `main` into `/tmp/go-shellies-extract/`
- [x] Run filter-repo with path filters and renames:
  ```
  --path pkg/shelly/
  --path pkg/devices/
  --path myhome/ctl/shelly/
  --path myhome/ctl/mcp/
  --path-rename pkg/shelly/:
  --path-rename pkg/devices/:devices/
  --path-rename myhome/ctl/shelly/:cmd/shelly/
  --path-rename myhome/ctl/mcp/:cmd/shelly/mcp/
  ```
- [x] Run filter-repo with `--replace-text` for import path rewrite:
  ```
  github.com/asnowfix/home-automation/pkg/shelly==>github.com/asnowfix/go-shellies
  github.com/asnowfix/home-automation/pkg/devices==>github.com/asnowfix/go-shellies/devices
  ```
- [x] Add seed remote, fetch `main`, `git merge --allow-unrelated-histories seed/main` to anchor LICENSE+README
- [x] Push as `feature/initial-library` on go-shellies

### Phase 3: standalone shelly CLI in go-shellies
- [x] Add `cmd/shelly/dispatch/` — resolves devices via `devices/` + `mdns.go` or hostname; opens `shttp` or `mqtt` channel; iterates over wildcards
- [x] Add `cmd/shelly/options/` — replaces `myhome/ctl/options`; no myhome config
- [x] Decouple each subcommand from `internal/myhome*`, `internal/tools`, `internal/shelly/scripts`, `myhome/mqtt`
- [x] Delete `cmd/shelly/{script,setup,follow}/` (out of scope for v2; history preserved by filter-repo)
- [x] Add `cmd/shelly/emulate/` — wraps `script/run.go` goja runtime; loads device-state + KVS JSON, runs script, prints captured event stream
- [x] Add `cmd/shelly/main.go` cobra root that registers all migrated subcommands + `cmd/shelly/cmd.go` `main()` entry
- [x] `go build ./...` and `go test ./...` pass for all 3 modules (root, script, gen1)
- [x] Update README.md with build/install/usage
- [x] Push commits on `feature/initial-library`

### Phase 4: home-automation rewire
- [x] On a fresh branch, push as `feature/migrate-pkg-shelly-in-its-own-repo` (re-using the original name now that v1 is rolled back)
- [x] `go.work`: remove `./pkg/shelly/*` and `./pkg/devices` entries; remove now-empty workspace modules
- [x] Add `replace github.com/asnowfix/go-shellies => /Users/fix/Desktop/GIT/go-shellies` for local dev
- [x] Rewrite ~90 import paths: `pkg/shelly` → `go-shellies`, `pkg/devices` → `go-shellies/devices`
- [x] `Init()` call sites pass `script.Init` via the extras callback (same as v1)
- [x] `git rm -r pkg/shelly/ pkg/devices/ myhome/ctl/mcp/`
- [x] `git rm -r myhome/ctl/shelly/{call,kvs,wifi,mqtt,sys,status,reboot,components,jobs}/`
- [x] Update `myhome/ctl/shelly/main.go` — register only `script`, `setup`, `follow`
- [x] `make build` and `make test` pass
- [x] Open PR

### Phase 5: cleanup
- [x] Delete `docs/plan-migrate-pkg-shelly.md` (the v1 plan, never made it to main)
- [x] Mark all phases done

## Module consolidation (unchanged from v1)

13 → 3 Go modules. Sub-packages of root: ble, blu, devices, ethernet, input, kvs, matter, mqtt, ratelimit, schedule, shelly, shttp, sswitch, system, temperature, types, wifi. Separate modules for `script/` (heavy goja+minify deps) and `gen1/` (gorilla/schema).

## Blockers (carried from v1, all resolved there — re-validate)

| # | File | Issue | Fix |
|---|------|-------|-----|
| 1 | device.go | dead `internal/myhome/net` import | filter-repo's `--replace-text` will rewrite the path string; the dead import line will simply be deleted in the de-coupling commit |
| 2 | shelly.go `Init()` | active `internal/shelly/scripts` import | already addressed in `pkg/shelly` HEAD via the `extras` callback pattern; same approach |
| 3 | mqtt/ops.go | deprecated `golang.org/x/exp/rand` | replace with `math/rand/v2` in de-coupling commit |
