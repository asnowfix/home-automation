# Migration Plan: Complete extraction to `github.com/asnowfix/go-shellies`

> Forward-looking tracker for the remaining work after the v2 history-preserving extraction.
> Earlier phases (filter-repo extraction, standalone `shelly` CLI, home-automation rewire)
> are documented and marked done in [`docs/plan-migrate-pkg-shelly-v2.md`](docs/plan-migrate-pkg-shelly-v2.md).

## Current state

- `go-shellies` repo: 3 modules (root, `script/`, `gen1/`); standalone `cmd/shelly` CLI
  with `call`, `kvs`, `wifi`, `mqtt`, `sys`, `status`, `reboot`, `components`, `jobs`,
  `mcp`, `emulate`, `version` subcommands. Pushed to `feature/initial-library`.
  No version tag yet. CI workflows present (`test.yml`, `release.yml`, `auto-tag-patch.yml`).
- `home-automation` repo: branch `feature/migrate-pkg-shelly-in-its-own-repo` rewires
  imports to `github.com/asnowfix/go-shellies/*`. `pkg/shelly/` and `pkg/devices/`
  removed. CLI keeps only `script`, `setup`, `follow`. `make build` and `make test` pass.
- Coupling: `home-automation/go.work` and `go.mod` use local-path `replace`
  directives pointing to `/Users/fix/Desktop/GIT/go-shellies` — the migration is
  not externally consumable yet.

## Phase 6: Land the migration

**Goal:** get both branches merged and replace local-path replaces with a tagged version.

### 6.1 Open the PRs

- [ ] Open PR on `asnowfix/go-shellies` for `feature/initial-library` → `main`
  - Title: `feat: initial standalone library + shelly CLI`
  - Body: summarize migrated modules, the 3-module layout, and the `shelly` binary subcommands
- [ ] Open PR on `asnowfix/home-automation` for `feature/migrate-pkg-shelly-in-its-own-repo` → `main`
  - Title: `refactor: migrate pkg/shelly to github.com/asnowfix/go-shellies`
  - Body: list deleted directories, new dependency, and that `script`/`setup`/`follow` stay
  - Mark draft until go-shellies tag exists

### 6.2 Tag go-shellies

- [ ] Verify `release.yml` works on a dry tag (or read it to understand the trigger)
- [ ] After go-shellies PR merges, tag `v0.1.0` on `main`
- [ ] Confirm Go module proxy serves it: `GOPROXY=proxy.golang.org go list -m github.com/asnowfix/go-shellies@v0.1.0`

### 6.3 Pin home-automation to a published version

- [ ] In `home-automation/go.mod`, replace pseudo-version with `v0.1.0` for:
  - `github.com/asnowfix/go-shellies`
  - `github.com/asnowfix/go-shellies/script`
  - `github.com/asnowfix/go-shellies/gen1`
- [ ] Same change in any other `go.mod` that requires those modules
  (search: `grep -rl 'go-shellies' --include=go.mod`)
- [ ] Remove the 3 local-path replaces from `go.work`
- [ ] `make tidy && make test && make build`
- [ ] Update home-automation PR (un-draft) and merge

**Checkpoint:** anyone can `git clone home-automation && make build` without a sibling
`go-shellies` checkout.

---

## Phase 7: Move deferred CLI subcommands to go-shellies

**Goal:** finish the v2 scope by moving the remaining generic CLI bits.
Doing this *after* Phase 6 because a tagged module makes iteration safer.

### 7.1 `follow`

- [ ] Audit `myhome/ctl/shelly/follow/` — what's MyHome-specific vs. generic?
  Generic: subscribing to a device's MQTT events. MyHome: registry / friendly names.
- [ ] Extract the generic MQTT-follow loop into `go-shellies/cmd/shelly/follow/`
- [ ] Use `dispatch.Lookup` to resolve devices (no daemon)
- [ ] Delete `myhome/ctl/shelly/follow/` if no MyHome-specific behaviour remains;
  otherwise keep a thin wrapper there

### 7.2 `script` (split, do not just move)

The `script` subcommand mixes generic (upload, list, status, eval) with
MyHome-specific (version tracking via embedded `internal/shelly/scripts`).

- [ ] Move generic operations to `go-shellies/cmd/shelly/script/`:
  - `upload`, `list`, `status`, `start`, `stop`, `eval`, `delete`
  - `upload` accepts a path argument; no embedded-FS lookup
- [ ] Keep MyHome-specific operations in home-automation under
  `myhome/ctl/shelly/script/`:
  - `update` (resolves embedded script by name + version-tracking via KVS)
  - the parts of `delete` that clean up KVS keys per script

### 7.3 `setup`

- [ ] Decide: stays MyHome-specific (uses `internal/myhome/shelly/setup` + LAN scan).
  Document that decision in this plan and remove from "deferred" list.

### 7.4 Eliminate `script.SetFS` coupling

After 7.2, `script.SetFS(scripts.GetFS())` in `daemon.go` and `myhome/ctl/ctl.go`
should only matter for MyHome's own embedded scripts. Confirm and either:

- [ ] Keep it: document why (MyHome embedded scripts FS for `update`)
- [ ] Or replace with an explicit parameter on the home-automation call site, so
  `go-shellies` no longer exposes a global FS setter

---

## Phase 8: Clean-up & docs

- [ ] Delete `docs/plan-migrate-pkg-shelly.md` (v1 plan, never landed) — Phase 5 in
  the v2 doc was marked done preemptively; verify it's actually deleted on `main`
- [ ] Update `AGENTS.md` "Three-Tier Layer Rule": `pkg/shelly/` row becomes
  "external dep `github.com/asnowfix/go-shellies/...`"
- [ ] Update `CLAUDE.md` "Architecture > Go Workspace": ~45 modules → new count;
  "Key Packages" table: drop the `pkg/shelly/script/` row (now external)
- [ ] go-shellies: add a `CHANGELOG.md` (start at `v0.1.0`)
- [ ] go-shellies: ensure GoDoc comments on all exported types — run
  `golangci-lint run --enable revive` and fix package-doc warnings

---

## Risks

| Risk | Mitigation |
|---|---|
| Tag-and-pin breaks tests because of accidental `v0.0.0-…` pseudo-versions in transitive go.mod files | Run `grep -rE 'go-shellies.*v0\.0\.0' --include=go.mod` after retagging — should return nothing |
| `release.yml` semantics unknown — might require a specific tag format | Read the workflow before tagging; do a `v0.0.1-rc.0` first if unsure |
| Splitting `script` subcommand breaks shell muscle memory | Keep wrapper aliases in `myhome/ctl/shelly/script/` until next major bump |
| Embedded-scripts FS coupling deeper than expected | Keep `SetFS` API stable in v0.x; revisit at v1.0 |
