# No IP addresses in the devices DB — implementation plan

Status doc for an in-progress feature. Delete this file once all phases are checked off and
merged. Update checkboxes as work lands so this can be resumed cold (fresh session / context
loss) by reading this file plus `git log` / `git diff` on this branch.

Branch/worktree: `feature+no-ip-address` (`.claude/worktrees/feature+no-ip-address`)

Implements: #252. Concrete symptom that motivated it: PR #265 (garden sprinkler `arrosage`
device) — device DHCP-renewed IP, daemon kept the stale cached IP, `shelly_list`/`shelly_call`
timed out over HTTP while MQTT kept working fine.

Deferred out of this issue, tracked separately: #336 (relaying HTTP through a NAT'ing gateway
device — Shelly AP/RangeExtender, or the TP-Link Omada EAP 100 at `192.168.1.4`). This plan's
`model.Router`/`model.Host` are scoped to **only ever return directly HTTP-dialable addresses**.

## Design decisions (confirmed in conversation before implementation)

1. MQTT stays the preferred channel (`Device.Channel()` priority unchanged). This work only
   fixes the HTTP path so it degrades correctly instead of getting stuck on a stale IP forever.
2. `internal/myhome/net/resolver.go`'s `Resolver.LookupHost` (DNS → mDNS `.local` fallback)
   becomes a second `model.Router` implementation, alongside the existing SFR ARP-table-style one
   (`internal/myhome/sfr/router.go`).
3. **Sequential fallback chain**, not concurrent fan-out. The SFR router is backed by an
   in-memory `sync.Map` refreshed by a background goroutine every minute — a lookup there is a
   fast in-process read, not a live network call. mDNS is the only hop with real network latency.
   Trying SFR first and only paying mDNS's cost on a miss is already near-optimal; concurrent
   fan-out would add goroutine/cancellation complexity for no measurable win.
4. Each `Router` implementation can be wrapped with a fallback: on a not-found result, try the
   next `Router` in the chain. Terminates in a `NotFoundRouter` that always returns not-found —
   this also lets call sites use a `Router` value unconditionally (no nil checks).
5. **No periodic MAC→IP polling.** The current background loop in
   `myhome/devices/impl/manager.go`'s `refreshOneDevice` (calls `router.GetHostByMac` once a
   refresh cycle and rewrites `device.Host()`) is removed entirely. Resolution becomes purely
   on-demand: lazily when a device has no known dialable IP, and once more on HTTP dial failure,
   before falling back to MQTT.
6. **No resolved IPs persisted to the devices DB.** The daemon stops writing live-resolved IPs
   into the `host` DB column. Resolution is always live via the `Router` chain, keyed by MAC
   (primary) and device ID (mDNS fallback — Shelly mDNS hostnames are `<device-id>.local`).
7. `model.Router`'s doc comment states the direct-dialable-only invariant (point above, re #336).

### Layering constraint (Three-Tier Layer Rule, see CLAUDE.md)

`model.Router` lives in `internal/myhome/model` (MyHome-layer). `pkg/shelly` must not import
`internal/myhome/*` packages directly. But the HTTP failure signal (and the "no known IP yet"
signal) both originate deep inside `pkg/shelly/shttp/channel.go` / `pkg/shelly/device.go`, and
that's exactly where a retry-after-re-resolution needs to happen to cover every caller uniformly.

Resolved via dependency inversion, following the existing `sfr.StatusReporter` package-level
injection pattern already used in this codebase:

- `pkg/shelly/types`: new minimal interface `HostResolver` (one method: resolve MAC/name → IP).
- `pkg/shelly`: package-level `var hostResolver types.HostResolver` + `SetHostResolver(r)`.
- `internal/myhome` (daemon wiring) builds the real `Router` chain and calls
  `shellyapi.SetHostResolver(...)` once at startup — the concrete `model.Router`-backed adapter
  lives in `internal/myhome`, so `pkg/shelly` never imports it.

## Progress

### Phase 0 — Setup
- [x] Worktree already exists, on branch `feature+no-ip-address`
- [x] Follow-up issue #336 filed and cross-linked from #252
- [x] `make generate` run successfully in this worktree

### Phase 1 — `model.Router` chain infrastructure — DONE
- [x] `internal/myhome/model/router.go`: added `ErrNotFound` sentinel + doc comment invariant
      (direct-dialable-only, see #336)
- [x] `internal/myhome/model/chain.go`: `Chain(primary, next Router) Router` — tries primary,
      falls through to `next` on **any** error (not just `ErrNotFound`; simpler and still correct
      since `NotFoundRouter` is always the terminal error)
- [x] `NotFoundRouter` terminal implementation
- [x] Unit tests (`chain_test.go`): primary-found, falls-through-on-error, terminates at
      `NotFoundRouter` with `ErrNotFound`

### Phase 2 — mDNS `model.Router` implementation — DONE
- [x] `internal/myhome/mdnsrouter/router.go`, implementing `model.Router`: `GetHostByName`
      resolves via an injected `mynet.Resolver` (constructor `New(resolver)`, not the package
      singleton directly — matches the existing `DeviceManager.resolver` injection pattern, and
      makes it testable with a fake); `GetHostByMac`/`GetHostByIp` return `ErrNotFound`
- [x] Unit tests (`router_test.go`) with a fake `mynet.Resolver`

### Phase 3 — `pkg/shelly` `HostResolver` seam — DONE
- [x] `pkg/shelly/types`: `HostResolver` interface + package-level `SetHostResolver`/`ResolveHost`
      (lives in `types`, not top-level `pkg/shelly`, so both `pkg/shelly` and the leaf
      `pkg/shelly/shttp` module can reach it without an import cycle)
- [x] `pkg/shelly.SetHostResolver` — thin forwarding alias for daemon-wiring ergonomics
- [x] `types.Device.Channel(ctx, via)` — **signature change** (added `ctx`, needed so resolution
      can happen before the channel-selection decision, not just after a dial failure; otherwise
      a device with no known host would always fall to the discarded `ChannelDefault` caller
      before ever reaching the HTTP dial code). Updated the 2 call sites
      (`ops.go` `CallE`, `device.go` `Refresh`) and the 3 implementations (`Device`, `FakeDevice`,
      test `fakeDevice`).
  - `Device.Channel`: when neither MQTT nor HTTP is ready, tries `resolveHost(ctx)` (via the
    injected resolver, keyed by MAC then device ID) before giving up to `ChannelDefault`. Skipped
    entirely when MQTT is already ready (preserves MQTT-preferred priority, no wasted resolution).
- [x] `pkg/shelly/shttp/channel.go` `callE`: on a dial failure, `ClearHost()`, ask the resolver
      once, `UpdateHost()` + retry on success; falls back to clear+error (→ MQTT) as before if the
      resolver is unset or the retry also fails
- [x] **Bonus fix found via testing**: `getE`/`postE`'s IPv6-bracket detection was buggy —
      `net.ParseIP` returns `nil` for a hostname or a `host:port` string, and `nil.To4() == nil`
      is unconditionally true, so non-IP-literal hosts were always (incorrectly) wrapped in
      `[...]` brackets. Replaced with a `formatHost` helper using `net.SplitHostPort`/
      `net.JoinHostPort`.
- [x] Unit tests: `device_test.go` (`Channel` resolves/doesn't-resolve/no-resolver cases),
      `shttp/channel_test.go` (success path unaffected, retry-after-failure calls resolver
      exactly once and updates/clears host correctly, no-resolver behavior preserved)
- [x] `make test` green across the whole workspace

### Phase 4 — Wire it up in the daemon — DONE
- [x] `myhome/devices/impl/manager.go` `Start()`: builds `model.Chain(sfr.GetRouter(ctx),
      model.Chain(mdnsrouter.New(dm.resolver), model.NotFoundRouter{}))` (or without SFR in
      remote-proxy mode), stored in `dm.router`, and calls
      `shelly.SetHostResolver(routerHostResolver{router: dm.router})`
- [x] New `routerHostResolver` adapter (in `manager.go`) implements `types.HostResolver` by
      trying `GetHostByMac` then `GetHostByName` on the chain — this is the internal/myhome-side
      half of the dependency-inversion seam from Phase 3
- [x] Removed the periodic MAC→IP polling block from `refreshOneDevice`; dropped its now-unused
      `router model.Router` parameter (and the call site in `deviceUpdaterLoop`)
- [x] `options.Flags.RemoteProxy` behavior preserved: SFR skipped in remote-proxy mode as before;
      mDNS `Router` is always included (harmless — it just returns `ErrNotFound` quickly if
      multicast can't reach the target network, no different from not having it)
- [x] `internal/myhome/shelly/gen1` listener's separate one-off `router.GetHostByIp` usage
      (MAC lookup from a Gen1 device's self-reported IP in its MQTT payload) is untouched — it's
      a different, legitimate use of `model.Router`, not the periodic-polling antipattern
- [x] `make build` + targeted `go test` green

### Phase 5 — Stop persisting IPs to the DB — DONE
- [x] `internal/myhome/device.go`: removed the 3 writes into `DeviceSummary.Host_` (persisted,
      `db:"host"`) — `Refresh()`, `WithZeroConfEntry()` (which still uses the mDNS entry's IP
      transiently, only as a lookup key into the SFR router to fill in MAC), and
      `NewDeviceFromImpl()` (first-discovery path). Any existing DB value is now legacy/inert and
      naturally ages out as devices get refreshed.
- [x] Audited all flagged read call sites:
  - `pkg/shelly/shelly.go:59` (`ShellyDevice.Ip()`) — **false positive**, wraps the live in-memory
    `pkg/shelly.Device`, not the DB `DeviceSummary`; unaffected, no change needed.
  - `internal/myhome/ui/template.go:138` — cosmetic-only fallback token (falls through to
    Name/Id when Host is empty, which it usually was already); left as-is.
  - `myhome/ctl/garden/setup.go`, `myhome/ctl/pool/{provider,setup}.go` — copy `Host_` into a
    passthrough `*myhome.Device` that every caller discards in favor of the live `*shelly.Device`
    obtained via `myhome.Foreach`/`GetShellyDevice` (keyed by device ID, goes through the
    HostResolver seam); confirmed unused for connectivity, left as-is.
  - `internal/myhome/proxy/reverse.go` `resolveToIPv4` — **real fix needed**: rewrote the
    DB-lookup fallback to try `Host()`, then `Id()` (guaranteed to match the mDNS `<id>.local`
    name), then `Name()`, instead of `Host()` then `Name()` then `Id()` — since `Host()` is now
    always empty, `Id()` needs to be tried before `Name()` for mDNS resolution to reliably work.
  - `myhome/ctl/open/main.go` — **real fix needed**: falls back to `<id>.local` when `Host()` is
    empty, so `myhome ctl open <name>` still produces a URL a browser can resolve (via OS-level
    mDNS/Bonjour) instead of `open http://` (empty host).
- [x] New/updated unit tests: `internal/myhome/proxy/reverse_test.go` (ID-before-Name fallback
      ordering, using a real in-memory `storage.DeviceStorage` + fake `mynet.Resolver`)
- [x] `make build` + `make test` green

### Phase 6 — Tests & verification — DONE
- [x] `make test` green across the whole workspace
- [x] The PR #265 stale-IP-self-heal scenario is covered deterministically by unit tests
      (`pkg/shelly/shttp/channel_test.go` `TestCallE_RetriesOnceAfterReResolution`) rather than
      reproduced against real hardware — deliberately not simulated live (would mean disrupting a
      working device's actual connectivity on the production network for the sake of the test)
- [x] Ran the built daemon locally (`go run ./myhome daemon run --instance local --mqtt-broker
      tcp://192.168.1.2:1883`, ~45s bounded run) against the real home MQTT broker: SFR router and
      mDNS resolver both start cleanly, no panics/errors from the new Router-chain/HostResolver
      wiring, real devices discovered via mDNS and successfully called over HTTP (e.g.
      `KVS.Get` against `192.168.1.37`, `192.168.1.36`) — confirms the happy path end-to-end
      against production hardware

## Out of scope (tracked elsewhere)

- Relay-through-gateway for NAT'd devices (Shelly AP/RangeExtender, Omada EAP 100) — #336.
- SNMP/direct ARP-table querying of arbitrary routers (only SFR's proprietary API is supported
  today) — not requested, no other router hardware in scope currently.
- Cover component support (roller shutters show no control in the UI) — #339. Discovered during
  manual UI verification; unrelated to host/IP resolution, connectivity to those devices works.

## Phase 7 — Live-testing follow-ups (post-implementation)

- [x] **UI "open device web interface" button had disappeared for every device.** Root cause:
      `internal/myhome/ui/htmx.go`'s device-card templates gated the button on `{{if .Host}}`,
      which is now always empty (Phase 5). Fixed by adding `DeviceView.HasWebUI` (true unless the
      device is BLU-only, which has no HTTP interface at all) and gating on that instead — the
      link itself (`/devices/{{.LinkToken}}/`) already resolves live via `resolveToIPv4` (fixed in
      Phase 5), so the button no longer needs a cached IP to know whether to render. Verified live:
      clicking through to a device's web UI succeeded (HTML page + a WebSocket upgrade both
      proxied correctly).
- [x] **Migrate legacy cached IPs out of the DB.** `myhome/storage/db.go`'s `createTable()` gained
      `migrateHostsToHostnames()`, run on every `NewDeviceStorage` open: any `host` value that
      parses as a literal IP (i.e. left over from before this PR) is rewritten to `<id>.local`.
      Idempotent, only touches rows with a literal-IP host, leaves everything else (empty, or
      already a hostname) untouched.
- [x] Added `pkg/shelly/types.ResolveHost` timing/outcome logging (mac, name, ip, err, elapsed) —
      needed to diagnose a reported "refresh button loops forever" issue live.
- [x] **Investigated a reported "refresh button loops forever" / then "1-2s per device" report.**
      Root-caused: not the new resolution code (confirmed via the new logging: zero invocations
      across the session — every tested device had MQTT ready, which is unconditionally preferred
      over HTTP, unchanged from before this PR). The one concretely observed slow case (~28s) was
      a pre-existing MQTT RPC timeout (`pkg/shelly/mqtt/channel.go`, 14s × 2 sequential calls) for
      a device that didn't answer at that moment — reproduced fast (0.3-0.4s) on retry. The
      settled 1-2s-per-device figure is consistent with two sequential MQTT round-trips plus the
      SSE-driven UI update, not obviously a regression from this PR. No code change made for this;
      flagged here in case it resurfaces with a specific reproducible device.

## Follow-up issues filed during this work

- #336 — relay support for devices NAT'ed behind a Shelly AP/RangeExtender or the TP-Link Omada
  EAP 100 (deferred `model.Host.Via()` design)
- #339 — Shelly Cover component support (roller shutters show no control in the UI)
