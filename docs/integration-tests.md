# Integration test campaign — real Shelly hardware

This is the living record for repeatable, low-risk integration test cases run against **real
Shelly hardware**, established by #422. It exists because the emulator-based unit tests
(`internal/shelly/scripts/pool_pump_test.go` against `pkg/shelly/script/run.go`'s goja emulator)
don't model every real-device constraint (see #250, open), and ad-hoc one-off live-device sessions
leave no repeatable record of what was checked or how.

Each test case below follows this template:

- **Script/version under test**
- **Purpose**
- **Preconditions**
- **Steps**
- **Pass/fail criteria**
- **Originating issue**
- **Last run**

No hardware-in-the-loop CI exists for this repo (hobby project, kept simple by design — see #422).
"Last run" is always a manually-recorded fact, never an automated badge. Re-verify preconditions
(device online, firmware version, KVS state) before trusting a stale "Last run" entry — this file
is a point-in-time snapshot, not live truth.

## Device availability — `development`

- **Name**: `development`
- **Shelly device ID**: `shellyplus1-08b61fd98f44`
- **Hardware**: Shelly **Plus 1** (single relay, single input)
- **Firmware**: `1.7.5` (`fw_id: 20260311-095850/1.7.5-g9979d16`), Gen2
- **MQTT broker**: `tcp://192.168.1.2:1883` — pass `--mqtt-broker tcp://192.168.1.2:1883` explicitly
  to every `myhome ctl shelly` command; mDNS autodiscovery is unreliable on the usual dev Mac.
- **Role**: dedicated spare integration-test target. Safe to crash, reboot, or reflash freely.
  Never confuse with `filtration-hiver` (`shellypro1-ec62608c0230`, the real pool pump — a Shelly
  **Pro1**, different hardware family, more script RAM — see the "Plus 1 vs Pro1/Pro3 memory" note
  in test case #1 below).
- It already runs two permanent scripts, script id 1 `watchdog.js` and script id 2
  `myhome-link.js`. Test scripts under evaluation should land at script id 3+ (whatever the device
  assigns next). **Deleting** either permanent script is still off-limits for any test case (it's
  real config the device is meant to run day-to-day) — but **stopping** them temporarily turned out
  to be *required*, not just tolerated, for test case #1 below: see "Shared JS heap" immediately
  below. Always restart both (`ctl shelly script start development watchdog.js` /
  `myhome-link.js`) at the end of a session that stopped them — a test case is not "done" while
  they're down.
- **Shared JS heap is small and shared across every script on the device**: this Plus 1's Gen2 JS
  engine gives all scripts on the device **one shared heap pool** (not one pool per script) —
  confirmed empirically in test case #1 by watching `Script.GetStatus`'s `mem_free` move in lockstep
  across script ids 1, 2, and 3 as scripts were stopped/started. Total pool size is roughly 25 KB
  (`watchdog.js` ~2.1 KB + `myhome-link.js` ~1.7 KB + ~21.3 KB free = ~25.2 KB with both idle-running;
  a script's own `mem_peak` while compiling/running eats further into whatever's left). A `pool-pump.js`
  build whose minified size approaches or exceeds the free remainder at upload time will fail to
  even compile (`out_of_memory`, zero log output, before the script's first line runs) if the other
  two scripts are left running. **Any future test case that uploads a script close to or above
  ~20 KB minified onto `development` must first stop `watchdog.js`/`myhome-link.js` to free enough
  heap**, and must restart them afterward.
- **Update, 2026-09-06 (re-verified read-only via `curl http://192.168.1.30/rpc/...` for this doc's
  refresh)**: the "Restore steps... have NOT been run" note below and test case #1's "Device end
  state" section describe this device as left with `pool-pump.js` crashed and not running. That is
  **no longer current** — a later campaign (#401) session reused this same rig, and as of today
  `Script.List` shows id 1 `watchdog.js` running, id 2 `myhome-link.js` **stopped**
  (`Script.GetStatus` reports `errors: ["out_of_memory"]`), and id 3 `pool-pump.js` **running**
  (`mem_used 15232`, `mem_peak 20608`, `mem_free 7798`). The shared-heap total implied by these
  numbers (`7798 + 2142 + 15232 ≈ 25172` bytes) still agrees with the ~25 KB figure recorded in
  2026-08 — that part of the finding holds. The specific device state (which script is running,
  which script id currently plays `pool-pump.js`) is not this PR's history to rewrite; treat this
  note, not the stale "Device end state" text above, as the last known state before starting any new
  test case here, and re-run `Script.List`/`Script.GetStatus` yourself regardless — this file is a
  point-in-time snapshot, per the header.

Environment gotchas that affect every test case run from a macOS dev machine on this network:

- **VPN default-route gotcha**: if the dev Mac has a VPN active (e.g. `utun4`), it wins the default
  route over the LAN interface (e.g. `en0`). `internal/myhome/net.MainInterface()` (used by
  `ctl shelly script debug <device> true`) picks the **VPN** address for the UDP debug callback,
  which the device cannot reach. Workaround used in this campaign: skip that CLI path's
  auto-binding entirely — start a plain `nc -u -l -k <port>` listener (see below) bound on all
  interfaces, then call `Sys.SetConfig` directly with `debug.udp.addr` set to the Mac's actual LAN
  IP (`ifconfig en0`) and the chosen port. Always verify with `Sys.GetConfig` that the address
  actually took, and check `restart_required` in the `Sys.SetConfig` response — only reboot the
  device if it says so.
- **macOS netcat gotcha — worse than it first looked**: `nc -u -l <port>` (without `-k`) stops
  accepting data after the first UDP "session" ends. `-k` was assumed to fix this, but empirically
  (this session, macOS 15.7.7) `nc -u -l -k <port>` **also** eventually stops accepting new
  datagrams after some point (confirmed by round-tripping manual test packets — only the first one
  after a (re)start of the listener got through). **Do not rely on `nc` at all** for a capture that
  needs to survive more than one burst of UDP traffic — use a small custom listener instead (this
  session used a ~20-line Python 3 `socket` script; see test case #1's "Debug capture" section for
  the exact script and verification method).
- **Script upload takes a bare embedded name, not a path**: `ctl shelly script upload <device>
  <script-name>` reads only from `internal/shelly/scripts/scripts.go`'s `//go:embed *.js` — it does
  **not** accept a filesystem path. Passing a path produces the misleading `Failed to read script
  <path>: file does not exist` (that's `io/fs` wording, not "no such file on disk"). To upload a
  specific version, checkout/edit that exact file in `internal/shelly/scripts/` and rebuild the
  binary (`make generate && make build`) — there is no way to point the uploader at an arbitrary
  file. Use `--force` to re-upload when the on-device KVS version hash already matches the content
  you're pushing (it will, on a repeat upload of unchanged content).

## Device availability — `filtration-hiver` (production pump — read-only checks only, by default)

- **Name**: `filtration-hiver`
- **Shelly device ID**: `shellypro1-ec62608c0230`
- **Hardware**: Shelly **Pro1** (single relay), a different app *and* firmware major from
  `development`'s Plus1/1.7.5 — a build that runs on `development` is not evidence it runs here.
- **Firmware**: `2.0.0` (`fw_id: 20260710-101206/2.0.0-g87fbfa4`), confirmed via
  `curl http://filtration-hiver.local/rpc/Shelly.GetDeviceInfo` on 2026-09-06.
- **Role**: **the real pool pump.** Standing authorization for hardware campaign work exists (see
  the coordinator's own memory), but this refresh session was scoped to **read-only checks only** —
  `Script.GetStatus`, `Switch.GetStatus`, `Shelly.GetDeviceInfo`, `Schedule.List` — no upload, no KVS
  write, no `Switch.Set`. Any test case below sourced from campaign #401 that ran writes against this
  device did so under that campaign's own authorization, in an earlier session; this doc only records
  the outcome.
- **There is no Pro1-on-2.0.0 test bed other than this device.** `development` (Plus1/1.7.5) and
  `mezzanine` (Pro1, but see below) are both used as proxies in various test cases; neither is
  equivalent to `filtration-hiver` for firmware-major-specific behaviour (see the `ReferenceError`
  test case below for a worked example of a "hard rule" that held on one platform and not the other
  until actually measured on both).

## Device availability — `mezzanine`

- **Name**: `mezzanine`
- **Shelly device ID**: `shellypro1-30c6f782d274`
- **Hardware**: Shelly **Pro1** — same hardware family as `filtration-hiver`, used in campaign #401
  as an isolation harness for `pool-pump.js` changes that should not touch the live pump. Several
  #401 test cases below configured it with production's full `script/pool-pump/*` KVS set
  specifically so it would be a valid proxy — see the "spare device is only a valid proxy if
  configuration matches" finding under `development`'s section above, which applies here too.
- **Role**: campaign-owned test bed, per the standing device-test-bed authorization. Not currently
  reachable from this read-only refresh session's checks (not probed — out of scope for this pass;
  all `mezzanine` figures below are sourced to their originating issue/PR comment, not re-measured).

## Known blocker — `ctl shelly script upload` / the `shelly` MCP server require a running daemon

**As of 2026-09-06, this blocks running any new device test case that needs an upload, KVS write, or
`Switch.Set` from this environment.** `ctl shelly script upload` and the `shelly` MCP server both
resolve devices through a daemon on instance `local` (`myhome-local.sh` execs `ctl --instance
local`), and none is running — calls fail with `timeout waiting for response to method
device.lookup after 14s (dst: local)`.

Starting a local daemon is **not** a free workaround: it also publishes
`myhome/energy/solar/available`, a second voice into the live pump's solar input, alongside whatever
device-management side effects daemon startup has. Read-only checks avoid this entirely by going
straight over HTTP: `curl http://<device>.local/rpc/<Method>` (or by IP — see `development`'s
mDNS-unreliability note above, which reproduced again during this session: `development.local`
resolved via `ping` but not via `curl`, needing `192.168.1.30` directly).

This is why this refresh session's own device checks (above) are `curl`-only, and why several test
cases below are recorded from campaign #401's own comments rather than re-run here.

---

## Test case #1 — reproduce and verify the fix for #421 ("Too many calls in progress" crash)

- **Script/version under test**: `internal/shelly/scripts/pool-pump.js` at commit `ae4c5da`
  (`main`, PR #419 "solar-driven start/stop hysteresis (#405)" — the tip of `main` at the time this
  baseline was recorded, i.e. **before** #421's fix has landed).

- **Purpose**: verify, on real Shelly Gen2 hardware, whether `pool-pump.js` crashes with `Uncaught
  Error: Too many calls in progress` (Bug A) during boot / tight restart cycles, per the incident
  described in #421. The emulator (`pkg/shelly/script/run.go`) cannot reproduce this today — it has
  no concurrency ceiling and no artificial async delay on `Shelly.call`, so nothing in it can ever
  trip a limit that isn't modeled (see #421's test plan, step 1, and #250). This is why real
  hardware is required for this specific test case.

- **Preconditions**:
  - `development` (`shellyplus1-08b61fd98f44`, Shelly Plus 1, fw 1.7.5) online and reachable via
    `tcp://192.168.1.2:1883`.
  - **`watchdog.js` (id 1) and `myhome-link.js` (id 2) stopped** (`Script.Stop`, not deleted) before
    starting `pool-pump.js`. This device's JS engine has a single ~25 KB heap shared by every
    script — with both permanent scripts running (~3.8 KB combined), only ~21 KB is free, which is
    *less* than the current minified `pool-pump.js` (39 KB), so the script cannot even compile in
    that state (see "Attempt 1" in Result below). Stopping both frees enough headroom
    (~25.2 KB) for `pool-pump.js` to boot and run. **Restart both at the end of the test** — see
    "Restore steps" below.
  - `pool-pump.js` uploaded to `development` as a **new** script (do not reuse/overwrite script ids
    1 or 2). Minified upload (`ctl shelly script upload development pool-pump.js`, no extra flags)
    — see "Bug found — `--no-minify` upload fails on this device" below for why unminified was
    attempted first and abandoned.
  - Required KVS key: `script/pool-pump/preferred` = `shellyplus1-08b61fd98f44` (this device's own
    ID — makes `isMyTurnToRun()` return true so it actually drives its own switch). This is the
    **only** KVS key `pool-pump.js` treats as `required: true` (see `CONFIG_SCHEMA.preferredDeviceId`
    in the script) — every other key in `CONFIG_SCHEMA` has a usable default and was left
    unconfigured for this test (defaults: `speed=eco`, `pool-volume=46`, `turnover=5`,
    `solar-enabled=false`, etc. — see `CONFIG_SCHEMA` in `pool-pump.js` for the full list and
    defaults if a future test case needs to override any of them).
  - Test `Schedule` jobs (short-interval, not the daily sunrise/sunset/23:15/00:15 cadence the
    script creates for itself on a successful boot) — created **manually** via raw `Schedule.Create`
    RPC calls because the script never reached its own `createSchedules()`/`verifySchedules()` step
    (see Result below): job id 2, `handleMorningStart()` at minutes `0,6,12,18,24,30,36,42,48,54`
    of every hour; job id 3, `handleEveningStop()` at minutes `3,9,15,21,27,33,39,45,51,57` — a
    3-minutes-on/3-minutes-off cadence. This spacing is deliberate: the script's own anti-cycling
    fuse (`FUSE_MAX_CHANGES = 4` changes per `FUSE_WINDOW_MS = 120000` i.e. 2 minutes) trips and
    force-turns-everything-off with a 5-minute cooldown if state changes happen faster than that;
    2 transitions per 6 minutes stays well clear of the fuse.
  - Continuous UDP debug capture running (see "Debug capture" below) so any crash's `error_msg`
    and surrounding log lines are recorded, not just the final `Script.GetStatus` snapshot.

- **Steps** (exact commands, run from repo root with the binary built via `make generate && make
  build`):
  ```bash
  # 1. Point device debug UDP output at a listener on the Mac's real LAN IP (NOT via
  #    `ctl shelly script debug`, which mis-binds to a VPN interface if one is active —
  #    see the VPN gotcha above). Replace 192.168.1.88 with `ifconfig en0`'s current address.
  #    Use a small custom UDP listener, NOT plain `nc -u -l -k` — see the netcat gotcha below,
  #    which turned out to be worse than originally documented (drops packets even with -k).
  python3 udp_capture.py 5099 development-debug.log &

  ./myhome/myhome ctl shelly call development Sys.SetConfig \
    '{"config":{"debug":{"mqtt":{"enable":false},"websocket":{"enable":false},"udp":{"addr":"192.168.1.88:5099","level":4}}}}' \
    --mqtt-broker tcp://192.168.1.2:1883

  # Verify it took, and reboot ONLY if restart_required was true in the response above:
  ./myhome/myhome ctl shelly call development Sys.GetConfig --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly reboot development --mqtt-broker tcp://192.168.1.2:1883   # only if needed

  # 2. Free the shared JS heap: stop the two permanent scripts (NOT delete). Confirm the
  #    mem_free jump via Script.GetStatus before proceeding.
  ./myhome/myhome ctl shelly script stop development watchdog.js --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly script stop development myhome-link.js --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly call development Script.GetStatus '{"id":1}' --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly call development Script.GetStatus '{"id":2}' --mqtt-broker tcp://192.168.1.2:1883

  # 3. Upload pool-pump.js from the exact commit under test (checkout that commit / ensure
  #    internal/shelly/scripts/pool-pump.js matches it, then `make generate && make build` first).
  ./myhome/myhome ctl shelly script upload development pool-pump.js \
    --mqtt-broker tcp://192.168.1.2:1883
  # (note the assigned script id, e.g. 3, from the "id: N" in the success message)

  # 4. Configure the one required KVS key (use the device's own ID and the id from step 3):
  ./myhome/myhome ctl shelly kvs set development script/pool-pump/preferred \
    shellyplus1-08b61fd98f44 --mqtt-broker tcp://192.168.1.2:1883

  # 5. Create short-interval test Schedule jobs (id substitutes the script id from step 3):
  ./myhome/myhome ctl shelly call development Schedule.Create \
    '{"enable":true,"timespec":"0 0,6,12,18,24,30,36,42,48,54 * * * SUN,MON,TUE,WED,THU,FRI,SAT","calls":[{"method":"script.eval","params":{"id":3,"code":"handleMorningStart()"}}]}' \
    --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly call development Schedule.Create \
    '{"enable":true,"timespec":"0 3,9,15,21,27,33,39,45,51,57 * * * SUN,MON,TUE,WED,THU,FRI,SAT","calls":[{"method":"script.eval","params":{"id":3,"code":"handleEveningStop()"}}]}' \
    --mqtt-broker tcp://192.168.1.2:1883

  # 6. Start the script and watch it (the device is briefly unresponsive to MQTT RPC for a few
  #    seconds right after Script.Start — use a longer --mqtt-timeout/-T and re-poll rather than
  #    concluding it's wedged):
  ./myhome/myhome ctl shelly script start development pool-pump.js --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly call development Script.GetStatus '{"id":3}' --mqtt-broker tcp://192.168.1.2:1883 -T 60s

  # 7. Simple tight stop/start restart cycling alone was NOT sufficient to trigger the crash in
  #    this session (23 consecutive cycles, no crash) — each externally-driven restart cycle
  #    apparently gives prior in-flight calls enough time to drain between my CLI round-trips.
  #    What DID reproduce it: force many of the script's own fire-and-forget KVS writes to
  #    queue up concurrently via a burst of parallel `script.eval` calls (bypasses the need to
  #    race restarts from outside — directly stresses the same storeValue()/queueTask() path
  #    #421 describes). Fire N calls in true parallel (background `&` + `wait` in a plain shell
  #    script — a for-loop of sequential foreground calls will NOT reproduce it):
  for i in $(seq 1 12); do
    ./myhome/myhome ctl shelly script eval development pool-pump.js \
      "persistRuntimeState($i); saveState();" --mqtt-broker tcp://192.168.1.2:1883 >/dev/null 2>&1 &
  done
  wait

  ./myhome/myhome ctl shelly call development Script.GetStatus '{"id":3}' --mqtt-broker tcp://192.168.1.2:1883
  # -> running: false, error_msg containing "Too many calls in progress" (see Result below)
  ```

- **Pass/fail criteria**:
  - **Fail (bug reproduced)**: `Script.GetStatus` for the pool-pump script returns `running: false`
    with `error_msg` containing `Too many calls in progress`, matching #421's reported signature.
  - **Pass (fix verified, for use once #421's fix lands)**: the script survives boot and repeated
    stop/start cycles without that crash; queued KVS writes (`storeValue` mirrors of
    `active-output`, `schedule-mode`, `runtime-sec`, `turnover-today`) all land correctly afterward
    (spot-check via `ctl shelly kvs get development "script/pool-pump/*"`); a restart mid-window
    self-corrects per Bug B's fix (separate assertion, not exercised by this run).
  - **Inconclusive / blocked**: script fails to even start for a reason unrelated to Bug A (this is
    what happened on this run's first attempt, before `watchdog.js`/`myhome-link.js` were stopped —
    see "Attempt 1" in Result below).

- **Originating issue**: #421 (crash), test-campaign process defined by #422.

- **Last run**: 2026-08-04, this session (branch `test/422-live-rig-421-repro`, worktree
  `.claude/worktrees/agent-a831607e74326abe0`, base commit `ae4c5da`). **Result: REPRODUCED.**
  `Script.GetStatus` returned `running: false` with `error_msg` starting `Uncaught Error: Too many
  calls in progress`, matching #421's signature exactly (see Result below for the full message,
  the exact recipe, and the earlier blocked attempt that preceded it).

### Result — Attempt 1 (blocked by out_of_memory) then Attempt 2 (crash reproduced)

This test case ran in two attempts within the same session. The first attempt, run with
`watchdog.js`/`myhome-link.js` left running (per this session's *original* instructions, since
lifted — see "Shared JS heap" above), could not even boot `pool-pump.js` and is preserved below as
a real, separate finding for #422's device-capacity planning. The second attempt, run after
stopping both scripts to free heap, successfully reproduced #421's crash.

#### Attempt 1 — `out_of_memory`, blocked before reaching Bug A's code path

`pool-pump.js` (current `main`, `ae4c5da`, includes #405's solar hysteresis) crashed with
**`out_of_memory`** the instant it was started on `development` with `watchdog.js` (id 1) and
`myhome-link.js` (id 2) both still running — before executing a single line of its own code, let
alone reaching the KVS-write-overlap code paths #421 describes.

**Evidence**:
- `Script.GetStatus {"id":3}` after every `Script.Start` attempt (3 attempts, across two debug
  levels):
  ```json
  {"id": 3, "running": false, "mem_free": 21350, "errors": ["out_of_memory"]}
  ```
  Identical result all 3 times — 100% reproducible, not transient.
- **Zero bytes** of new UDP debug output for script id 3 across all 3 attempts, including one at
  the debug UDP's maximum verbosity (`level: 4`, matching what `ctl shelly script debug` itself
  uses). `pool-pump.js`'s very first executable statement is `initConfig()` at file scope
  (populates `CONFIG` from `CONFIG_SCHEMA` defaults, no RPC), followed by `init()` at end-of-file
  calling `log("Script starting...")` as its first action — neither ever printed anything. This
  places the crash during **bytecode compilation/load**, before any of the script's own JS runs.
- `mem_free: 21350` (~21 KB) is **identical** across all three scripts on the device — script id 1
  (`watchdog.js`, `mem_used: 2142`), id 2 (`myhome-link.js`, `mem_used: 1680`), and id 3
  (crashed) all report the same `mem_free`. This confirms the JS engine heap is a single pool
  **shared across every script on the device**, not per-script — consistent with
  `reference_shelly_pro3_limits.md`'s documented "~30 KB heap" for the Pro3, and implying an even
  smaller total pool on this Plus 1 (roughly `21350 + 2142 + 1680 ≈ 25 KB` total, with `watchdog.js`
  + `myhome-link.js` already consuming ~3.8 KB of it).
- `Script.GetCode {"id":3}` returns **39127 bytes** of minified JS (the on-device minified size of
  current `main`'s `pool-pump.js`, post-#405). That single script, on its own, is larger than the
  device's entire shared JS heap pool — it cannot possibly compile into the ~21 KB currently free,
  independent of whatever `watchdog.js`/`myhome-link.js` are also using.
- Device-level `Sys.GetStatus.sys.ram_free` was `133536` bytes (130 KB) at the same moment — plenty
  of *system* RAM free. The constraint is specifically the JS engine's own fixed script-heap
  partition, not overall device memory pressure.

**Secondary finding — `--no-minify` upload fails on this device** (attempted first, per this
campaign's instructions, since unminified gives far better stack traces): `ctl shelly script
upload development pool-pump.js --no-minify` fails with:
```
device replied error 'Missing or bad argument 'code'!' (code:-103) ... index:53248
```
This is a different failure from the known "doesn't fit device's script **storage**" issue noted in
#421 for the Pro1 (that's a `fs_free` limit; here `fs_free` was `69632` bytes at the time, plenty of
room for the ~76 KB unminified source — `fs_size` is `393216`). This instead looks like a chunked
Put-code sequencing bug (the upload stalls at a fixed byte offset, 53248, with an RPC arg-shape
error), independent of Bug A/B. Not investigated further — out of scope for this session (the fix
agent isn't touching upload chunking, and minified upload succeeded once retried) — but worth a
follow-up issue if unminified live-debugging is needed again on this class of device.

**Conclusion of Attempt 1**: with `watchdog.js` + `myhome-link.js` both running, this Plus 1 cannot
run current-`main` `pool-pump.js` at all, because the script's minified size (39 KB) alone exceeds
the device's ~21 KB of free shared JS heap in that state. This blocked reaching Bug A's crash site
entirely on the first attempt — resolved by freeing the heap, see Attempt 2 below.

#### Heap evidence — the shared-pool theory, confirmed (not just inferred)

Stopping `watchdog.js` (id 1) and `myhome-link.js` (id 2) — `Script.Stop`, **not** deleted —
between the two attempts gave a clean before/after measurement:

| State | `Script.GetStatus` `mem_free` (ids 1, 2, 3 all report the same value) |
|---|---|
| Both scripts running, id 3 not yet started (Attempt 1) | `21350` bytes |
| Both scripts **stopped** | `25200` bytes |
| Difference | `3850` bytes freed — matches `watchdog.js`'s `mem_used: 2142` + `myhome-link.js`'s `mem_used: 1680` = `3822` (small variance is normal engine bookkeeping overhead) |

This is now a **confirmed fact**, not an inference: the JS engine on this device gives every
script — regardless of id or running state — the *same* `mem_free` reading at any given instant,
because they draw from one shared heap pool. Freeing 3850 bytes by stopping two small scripts was
just barely enough (25200 free vs. 39 KB of minified `pool-pump.js`) for `pool-pump.js` to actually
compile and start — see Attempt 2.

Useful comparison from the coordinator's own concurrent work on `filtration-hiver` (Shelly **Pro1**,
`shellypro1-ec62608c0230`): the *same* 39 KB minified script reported `mem_free: 23044` **while
already running and crashed** on that device — i.e. after paying the compile cost, running its full
init sequence, and holding whatever runtime state it accumulated before crashing, the Pro1 still had
more free heap than this Plus 1 has *before compiling anything*. This strongly suggests the Pro1's
total JS heap pool is substantially larger than the Plus 1's ~25 KB, consistent with the Pro-series
using a more capable MCU. The Plus 1's problem in Attempt 1 was primarily the two resident scripts
competing for a small shared pool, not an inherent inability of Plus-class hardware to ever run this
script — Attempt 2 (below) proves the exact same script binary boots and runs fine on this exact
Plus 1 once that headroom is freed.

#### Attempt 2 — crash reproduced, after stopping `watchdog.js`/`myhome-link.js`

With both permanent scripts stopped, `pool-pump.js` (script id 3) started successfully:

```json
{"id": 3, "running": true, "mem_used": 12572, "mem_peak": 22204, "mem_free": 12614}
```

Full boot log (KVS loads, `Step 1/4`...`Step 4/4`, `Script initialization complete`) was captured —
see "Debug capture" below. The switch never turned on during this test case (no `doStart()` call
was exercised — see Preconditions' schedule-cadence note; this run targeted Bug A only).

**Simple restart cycling did not reproduce the crash.** 23 consecutive `Script.Stop` +
`Script.Start` cycles (8 with a full external round-trip per command, 15 more back-to-back) all
left the script running normally afterward (`mem_used` climbed slightly, 12572 → 15708, but stayed
stable — no leak-to-crash observed over these cycles). This is consistent with #421's own framing:
the crash needs several of the script's *internal* triggers to overlap within roughly a 200ms
window (the `TASK_QUEUE` tick), not just an external stop/start race — and my CLI round-trips over
MQTT apparently gave prior in-flight `Shelly.call`s enough time to drain between cycles.

**What did reproduce it**: firing a burst of 12 *concurrent* `Script.Eval` calls (`persistRuntimeState($i);
saveState();` for `$i` 1–12, launched together via background `&` + `wait`, not sequentially) —
directly forcing many of the script's own fire-and-forget `storeValue()` writes to queue up within
the same window, the same mechanism #421 describes for the boot-time `continueInit()` sequence.
This succeeded on the **first** such burst attempt. Resulting status:

```json
{
  "id": 3,
  "running": false,
  "errors": ["error"],
  "mem_free": 25200,
  "error_msg": "Uncaught Error: Too many calls in progress\n at ...ONFIG_KEY_PREFIX+e,value:n})\n                              ^\nin function \"storeValue\" called from storeValue(\"runtime-sec\",Math.round(t))\n                                      ^\nin function \"e\" called from ...TASK_INDEX];TASK_INDEX++,e()\n                              ^\nin function called from system\n\n"
}
```

This **exactly matches #421's reported crash signature** (`Uncaught Error: Too many calls in
progress`, thrown from inside `storeValue`, called from the `TASK_QUEUE` dispatcher
`...TASK_INDEX];TASK_INDEX++,e()`). The call site here (`storeValue("runtime-sec", ...)`, inside
`persistRuntimeState`) is a **third** distinct call site hitting the same crash class, alongside
the two #421 already reported (`turnover-today` in `persistRuntimeState`, and the active-output
mirror in `saveState`) — further confirming #421's framing that this is systemic to how
`queueTask`/`storeValue` interact, not tied to one specific call.

Surrounding debug log (device-local time, `uptime` field is device seconds-since-boot) captured via
the working UDP pipeline, trimmed to the crash window:

```
1180.218  Script.Eval [...] via MQTT / KVS.Set [...] via loopback
1180.346  ^
1180.346  in function "e" called from ...TASK_INDEX];TASK_INDEX++,e()
1180.346  ^
1180.346  in function called from system
1180.346  shelly_espruino.cpp:264  Caught error: 9 error
1180.346  shelly_espruino.cpp:275  JS Error [9] error Error in EjsCall used=1158 peak=1586 total=1159
1180.346  shelly_user_script.:236  UserScript.HandleError (script:3) [9] error Error in EjsCall
1180.536  Script.Eval [...] via MQTT / KVS.Set [...] via loopback / kvs_rev:41 / kvs_rev:42
1180.615  Script.Eval [...] via MQTT / "No call record for 324" / Status change of script:3:
           {"error_msg":"Uncaught Error: Too many calls in progress...","errors":["error"],"running":false}
1180.818  KVS.Set [...] via loopback (x2, still draining from before the crash)
1180.869  kvs_rev:43 / kvs_rev:44 / "No call record for 325"
1180.890  "No call record for 326" / "No call record for 327"
1180.902  "No call record for 328"
```

The repeated `"No call record for <N>"` lines after the crash are the firmware's own RPC layer
discovering that responses it's still trying to deliver belong to calls whose originating context
(the now-dead script instance) no longer exists — i.e. exactly the "calls still draining after the
script died" behavior #421 describes as the root mechanism (in-flight calls outliving the
JS-visible bookkeeping that was supposed to track them).

**Reproduction summary**: took 1 burst (12 concurrent `Script.Eval` calls) on the first attempt,
after 23 plain restart cycles had failed to trigger it. Crash call site: `storeValue("runtime-sec",
...)` inside `persistRuntimeState()`, dispatched from the `TASK_QUEUE` timer.

**Device end state after this test run**: `development` has script id 3 = `pool-pump.js` (current
`main`, `ae4c5da`) uploaded, **not running** (crashed with the `error_msg` above). `watchdog.js`
(id 1) and `myhome-link.js` (id 2) are **still stopped** — not yet restarted, see "Restore steps"
below. KVS `script/pool-pump/preferred` = `shellyplus1-08b61fd98f44` still set. Two test `Schedule`
jobs (ids 2 and 3) still present. Physical switch confirmed **off**
(`Switch.GetStatus {"id":0}` → `"output": false`) — no code path in this test case ever called
`doStart()`. Device-level UDP debug remains enabled, targeting `192.168.1.88:5099` at `level: 4`.

### Restore steps (run these to return `development` to its pre-session state)

```bash
BROKER=tcp://192.168.1.2:1883

# 1. Restart the two permanent scripts.
./myhome/myhome ctl shelly script start development watchdog.js --mqtt-broker $BROKER
./myhome/myhome ctl shelly script start development myhome-link.js --mqtt-broker $BROKER

# 2. Remove the test Schedule jobs (ids 2 and 3 — verify with Schedule.List first in case IDs
#    drifted from a later session; never delete job id 1, the firmware auto-update schedule).
./myhome/myhome ctl shelly call development Schedule.Delete '{"id":2}' --mqtt-broker $BROKER
./myhome/myhome ctl shelly call development Schedule.Delete '{"id":3}' --mqtt-broker $BROKER

# 3. Clear the KVS preferred key (only required key that was set).
./myhome/myhome ctl shelly kvs delete development script/pool-pump/preferred --mqtt-broker $BROKER

# 4. Optional: stop and/or delete pool-pump.js (script id 3) if development is being freed up
#    entirely rather than kept as a standing pool-pump test target.
./myhome/myhome ctl shelly script stop development pool-pump.js --mqtt-broker $BROKER
./myhome/myhome ctl shelly script delete development pool-pump.js --mqtt-broker $BROKER

# 5. Verify final state.
./myhome/myhome ctl shelly script status development --mqtt-broker $BROKER
./myhome/myhome ctl shelly kvs get development "script/pool-pump/*" --mqtt-broker $BROKER
./myhome/myhome ctl shelly call development Schedule.List --mqtt-broker $BROKER
```

**As of this doc's last edit, steps 1–3 above have NOT been run** — `development` was deliberately
left with `watchdog.js`/`myhome-link.js` stopped and the test config in place, so a follow-up
session (e.g. verifying #421's fix once it lands) can reuse the same rig without re-provisioning.
Whoever next declares this device "done" for the current campaign phase should run the restore
steps (or explicitly re-document why the rig is being kept live again, as this note does).

### Debug capture (raw UDP dump, continuous)

**Update — the originally documented `nc -u -l -k` gotcha was worse than described, and `nc` was
replaced with a small Python listener partway through this session.** First listener: `nc -u -l -k
5099` (PID 14982, started 08:17:39), which worked initially (captured the `Sys.SetConfig`/reboot
sequence in Attempt 1) but then went silent for good — **even with `-k`**, this macOS BSD `nc` build
stopped accepting new datagrams after some point (confirmed by sending manual test packets with a
second `nc -u` client from the same Mac: only the very first test packet after a restart of the
listener got through; a second one sent seconds later, from the same source, was silently dropped).
This is a **stronger** version of the previously-documented gotcha — `-k` does not reliably fix it
on this macOS version (`15.7.7`), it just delays the point of failure. **Do not trust `nc -u -l -k`
for a debug capture that needs to survive more than one burst of traffic.**

**Working replacement**: a ~20-line Python 3 UDP listener
(`/Users/fix/tmp-claude/claude-501/.../scratchpad/udp_capture.py` in this session — copy it
wherever convenient, it has no dependencies beyond the stdlib `socket` module), run as
`python3 udp_capture.py <port> <logfile>`. It binds `0.0.0.0:<port>` and appends every datagram to
the log file with a wall-clock timestamp and source address, forever, with no session/latching
behavior. Verified with 5 manual test packets sent seconds apart from different ephemeral source
ports — all 5 arrived, none dropped. This is what actually captured Attempt 2's full boot log and
crash `error_msg`.

- **Listener**: `python3 udp_capture.py 5099 <logfile>`, PID `23594`, started 2026-08-04 08:27:00
  local time. Still running as of this doc's last edit.
- **Log file** (current, working): `/Users/fix/tmp-claude/claude-501/-Users-fix-Desktop-GIT-home-automation--claude-worktrees-bugfix-pool-pump-not-starting/50548ab1-7478-4d25-8017-440e5aaba035/scratchpad/live-logs/development-udp-python-20260804-082700.log`
  — human-readable text, one `--- <timestamp> from <ip>:<port> ---` header per datagram followed by
  the raw device debug line(s) (device firmware format: `<device> <msgCount> <timestamp>
  <component> <level>|<message>`, multiple records sometimes concatenated in one datagram —
  no further parsing needed to read it, unlike the old raw-`nc` dump).
- **Superseded log file** (Attempt 1 only, `nc`-based, stopped receiving after ~08:18): `/Users/fix/tmp-claude/claude-501/-Users-fix-Desktop-GIT-home-automation--claude-worktrees-bugfix-pool-pump-not-starting/50548ab1-7478-4d25-8017-440e5aaba035/scratchpad/live-logs/development-udp-raw-20260804-081739.log`
  — kept for the record (contains the `Sys.SetConfig`/reboot sequence from setup), but do not
  expect it to have anything after Attempt 1's OOM crash attempts (it doesn't — that absence is
  itself part of Attempt 1's evidence chain, not a capture failure at that point in time).
- Device-side config unchanged throughout: `Sys.GetConfig` → `debug.udp.addr =
  "192.168.1.88:5099"`, `level: 4` — same port, only the listening process on the Mac side changed.
- **As of this doc's last edit, the Python capture (PID 23594) is still running.** Stop it with
  `kill 23594` (or `pgrep -f udp_capture.py` if the session has moved on) once no longer needed —
  it has no `-k`-style gotcha, so it's safe to leave running indefinitely.

---

## Test case #2 — Gate 3, solar extension and stop, full-day production run

- **Script/version under test**: `pool-pump.js` at commit `39815c12` (post-#402/#403/#405, `main` at
  the time), running as script id 2 on `filtration-hiver`.

- **Purpose**: verify, on the real production pump, that `solar-enabled = true` lets solar hold the
  pump running past its scheduled stop and release it cleanly when generation fades — the core
  capability #401 was opened to deliver, and one that cannot be observed any other way (there is no
  Pro1-on-2.0.0 test bed; see the device-availability note above).

- **Preconditions**: `solar-enabled = true` on `filtration-hiver`; a clear-enough day for solar
  output to exceed the (lowered, for this run) 200 W start / 150 W stop thresholds after the
  scheduled window closes; continuous 5-minute manual monitoring for the full day (no automated
  monitor tool was trusted for this — `tools/monitor-solar.sh`, #546, is separately documented as
  having produced zero bytes of output for 1h44m during a later session, see the ReferenceError test
  case below).

- **Steps**: set `solar-enabled = true`, restart the script, let the daily schedule run normally,
  observe `Script.GetStatus`/relay state/solar feed manually through and past the scheduled stop
  time, until solar itself releases the pump.

- **Pass/fail criteria**:
  - **Pass**: relay stays on past the scheduled stop while solar remains above the stop threshold,
    releases within one 10-minute confirmation delay of solar dropping below it, actual runtime
    lands within the day's required-hours/turnover band, zero script crashes, no rapid relay
    cycling.
  - **Fail**: crash, cycling (fuse trip), or turnover band missed.

- **Originating issue**: #401 (campaign), addresses the Gate 3 "extension and stop" half of the
  campaign's stated objective.

- **Last run**: **2026-08-24**, `filtration-hiver` (Pro1, fw 2.0.0). **Result: PASSED.**
  `solar-enabled = true` from 05:33 CEST. Scheduled window 10:48–15:12 (4.40 h required by
  `computeRunHours(28.9°C)`); solar crossed the (lowered) 200 W start threshold ~11:04, peaked at
  840 W at 12:49; scheduled stop at 15:12 was overridden by solar; solar dropped below the 150 W
  stop threshold at 18:20 (after a 10-minute confirmation delay) and the pump stopped cleanly.
  **Actual runtime 7.53 h (27096 s credited) against 4.40 h required — solar extension +3.13 h.**
  Turnover landed inside the 5–7 band (`solarHardCeilingReached = false`). **0 script crashes, 2
  relay transitions** (one on, one off — no cycling), water-supply interlock never fired, heap flat
  all day (`mem_used`/`mem_free` 16128/6174, no leak on the per-message solar path). Source: issue
  #401, comment posted 2026-08-24T17:34:45Z.
  - **Not exercised by this run**: the solar *start* path — the schedule, not solar, started the
    pump at 10:48 that day (structural: `handleDailyCheck()` sizes the run window from mid-morning,
    so on a clear day the schedule starts the pump before solar crosses the threshold). See test
    case #3.

---

## Test case #3 — Gate 3, solar start, production run

- **Script/version under test**: `pool-pump.js` current at the time (`main`, post-#476 reconciler),
  script id on `filtration-hiver`.

- **Purpose**: verify that solar alone — with no other branch of `desiredOutput()` able to have done
  it — can start the pump on the real production device. This is the half of Gate 3 that test case
  #2 above explicitly did not exercise, and it could not be exercised passively: `handleDailyCheck()`
  sizes the run window from mid-morning, so on an ordinary clear day the **schedule** starts the pump
  before solar crosses the threshold, making a naturally-occurring solar-only start structurally
  unreachable without intervention.

- **Preconditions**: a day with solar available before the scheduled window would normally open;
  `F_WIN_START` deliberately pushed **beyond** the expected solar-crossing time (not simply disabling
  `handleMorningStart()` — that method was tried and rejected first, see Result below, because window
  membership is itself a level that the #476 reconciler drives independently of the schedule job);
  the solar start threshold temporarily lowered if the live solar feed's noise band sits close to the
  configured default (see the "500 W default is a trough, not a mean" finding below); abort criteria
  from test case #2 unchanged (two relay transitions inside 5 minutes trips the fuse; treat as an
  abort signal).

- **Steps**: push `F_WIN_START` past the point solar is expected to cross the (lowered) start
  threshold; wait for `F_SOLAR_WANT` to flip to `true` and the relay to close; confirm via
  `desiredOutput()`'s three branches (override, solar-want, window-membership) that only the
  solar-want branch could have fired; restore `F_WIN_START`, the threshold, and
  `handleMorningStart()`'s enable flag afterward.

- **Pass/fail criteria**:
  - **Pass**: relay closes while override is inactive and window-membership is false, i.e. the
    solar-want branch is the only one that could have produced the transition; no fuse trip; runtime
    accounting unaffected.
  - **Fail**: pump starts via the schedule/window branch (proves nothing about solar) or does not
    start at all within the observation window.

- **Originating issue**: #401 (campaign), addresses the Gate 3 "start" half.

- **Last run**: **2026-08-30 12:53:54 CEST**, `filtration-hiver` (Pro1, fw 2.0.0). **Result: PASSED**
  — first success in four attempts. `F_WIN_START` pushed to 960 (16:00); at 12:53:53 `F_SOLAR_WANT`
  flipped to `true` (solar 420 W, threshold lowered to 300 W); `switch.on` fired one second later, at
  12:53:54. At that instant: override branch inactive (`F_OVR_WANT = -2`); window-membership branch
  false (`F_WIN_START = 960` i.e. 16:00, clock read 12:53); solar-want branch was therefore the
  **only** branch that could have produced the transition. Relay transitions 13 min 32 s apart (abort
  criterion "<5 min between transitions" not tripped); `turnover_today` showed no filtration at risk.
  Source: issue #401, comment posted 2026-08-30T10:55:54Z.
  - **Design finding from this run, more consequential than the pass itself**: the live solar feed
    alternated 360↔420 W every ~45 s. The start-side code (`if (SOLAR.availableW <
    CONFIG.solarStartThresholdW) { SOLAR.aboveStartSince = 0; ... }`) resets its 5-minute
    accumulation hold on **any single sample** below threshold, with no debounce or averaging. A
    threshold sitting inside the feed's noise band (e.g. the shipped default of 500 W against an
    observed ~390 W mean with a 360 W trough) is therefore **structurally unreachable** — the hold
    can never accumulate 5 uninterrupted minutes. Lowering the threshold to 300 W, below the observed
    trough, let every sample clear it. **The threshold is a trough setting, not a "how much sun do
    you want" mean setting**, and this is not currently documented anywhere the config schema
    explains it.
  - **Method correction, recorded because it is reusable**: the originally-planned method (disable
    `handleMorningStart()` and see if solar starts the pump on its own) was tried first and does
    **not** isolate solar — with the job verifiably disabled, the pump still started at 11:49:24
    (the exact window-start minute) with `F_SOLAR_WANT = false`, because window membership is a level
    the #476 reconciler drives independently of any schedule job. The valid method is to push
    `F_WIN_START` itself past the expected solar crossing.

---

## Test case #4 — `ReferenceError` inside `try`/`catch`: containment corrected on both platforms

- **Script/version under test**: `pool-pump.js` post-#574 (mandatory `try`/`catch` wrapping around
  `script.eval`-dispatched schedule jobs, per #528's fix).

- **Purpose**: settle whether a `ReferenceError` thrown inside a wrapped `try` block is actually
  caught on this firmware, or escapes and kills the script. This directly overturned a previously
  recorded "hard rule" (issue #559 §7) that argued *against* the wrapping #574 shipped — a concrete
  instance of why this log needs to exist rather than trusting an unreproduced incident write-up (see
  CLAUDE.md's `events.db` section, "worked example, #523").

- **Preconditions**: none beyond a script already running the `try`/`catch`-wrapped dispatch code;
  a safe way to reference a non-existent identifier without crashing the *caller* — the
  `typeof`-guarded, quote-free probe form (`typeof X === typeof u` with `var u` left undefined) was
  used precisely because it needs no string escaping when passed through `Script.Eval`'s JSON
  argument, avoiding an unrelated failure mode.

- **Steps**: from a live `Script.Eval` call (both a direct call form and a bare-reference form),
  reference an identifier that does not exist inside code already wrapped in a mandatory
  `try`/`catch`; observe whether the script survives and what value the `catch` receives.

- **Pass/fail criteria**:
  - **Pass (containment holds)**: the script remains `running: true` after the throw; the error
    surfaces as an ordinary value inside the catch handler (e.g. returned/logged), not as a script
    crash.
  - **Fail (matches the old #559 §7 rule)**: the script dies (`running: false`), i.e. the
    `ReferenceError` escaped the `try`.

- **Originating issue**: #401 (campaign); corrects #559 §7 (superseded by #594, which records this
  finding); the operator-induced incident that originally motivated #559 §7 is separately noted as
  unreproduced below.

- **Last run**: **2026-09-02**, run on **both** `development` (Plus1, fw 1.7.5, call and
  bare-reference forms) and `filtration-hiver` (Pro1, fw 2.0.0, production). **Result: PASSED on
  both** — the `ReferenceError` (`ReferenceError: "to" is not defined` on `filtration-hiver`) was
  returned as a value with the script remaining alive, on both platforms. Source: issue #594's body
  (which supersedes #559, closed) and issue #401 comment 2026-09-02T18:34:37Z ("#559 §7 recorded the
  opposite as a hard rule, from the 2026-08-30 incident. It does not reproduce.").
  - **Correction this test case makes to prior documentation**: #559 §7 had recorded, as a hard
    kill-list rule from one 2026-08-30 incident (an operator probe that killed the script, restarted
    10:14:16, settled `mem_free` 7420), that a `ReferenceError` escapes `try`/`catch` on this
    firmware. **That rule is wrong and does not reproduce.** What actually killed the script on
    2026-08-30 remains open (the reference in that incident probably sat outside the `try`, not
    inside it) — the rule as stated, "wrap nothing, it won't help," was backwards, and would have
    argued against the exact wrapping (#574) this test case shows works.

---

## Test case #5 — Shelly KVS key length limit (42 characters, error `-103`)

- **Script/version under test**: PR #537's `storeValue("runtime-last-rollover-discarded", sec)` call
  (`internal/shelly/scripts/pool-pump.js`), which built the KVS key
  `script/pool-pump/runtime-last-rollover-discarded` — 48 characters.

- **Purpose**: verify empirically whether Shelly Pro1 firmware enforces a KVS key length limit, after
  a `make test`-green, code-reviewed PR's `storeValue()` write was suspected of silently failing
  (issue #540) because `storeValue()` has no error handling on the underlying `KVS.Set` RPC.

- **Preconditions**: `mezzanine` (Shelly Pro1, `shellypro1-30c6f782d274`) online, running the build
  under test.

- **Steps**: attempt the write both through the script's own `storeValue()` call and through a
  direct `KVS.Set` RPC with an explicit callback that surfaces the RPC error; follow with `KVS.List`
  to check whether the key exists at all.

- **Pass/fail criteria**:
  - **Fail (bug present)**: `KVS.Set` returns an RPC error and/or `KVS.List` never shows the key,
    despite `storeValue()` (or `make test`, or code review) reporting no problem.
  - **Pass**: key appears in `KVS.List` with the expected value.

- **Originating issue**: #540, hardware-verified as part of #401.

- **Last run**: **2026-08-23**, `mezzanine` (Pro1). **Result: FAIL — confirmed bug.** Both the
  script's own write and a direct `KVS.Set` with an explicit callback returned:
  ```
  -103: "Invalid argument 'key': length should be less than 42!"
  ```
  reproduced twice. `KVS.List` confirmed the key was **permanently absent** — the write had a 0%
  success rate on every attempt, silently, because `storeValue()` discards the RPC error. `make
  test` was green throughout (the goja emulator models neither firmware key limits nor stack depth,
  #496) and a code review passed the PR — neither safety net could have caught this; only the
  device's own `KVS.List` did. Source: issue #540, comment 2026-08-23T13:13:23Z.
  - **Rule that follows, applicable to any future `storeValue()`/`KVS.Set` addition**: count the
    **full** key including its `script/pool-pump/` prefix (17 characters) before writing the code —
    `<name>` must stay at or under 24 characters — and verify the key actually appears in `KVS.List`
    on a device afterward. A successful-looking call from `storeValue()` is not evidence; it is
    exactly the same trap class as the `ctl shelly script upload` embedded-copy trap documented
    above (the tool reports success while doing nothing).

---

## What is NOT verified on hardware — first-class, not a footnote

A reader of this log needs to be able to see, at a glance, which merged changes are still unproven
on real devices. This section exists for exactly that; add an entry here at merge time for any
device-script or daemon change that has not yet had a hardware run, and move it up into a numbered
test case once one happens.

### PR #587 / commit `f380a143` — pump event reason codes — **never run on any device**

- **What merged**: `internal/shelly/scripts/pool-pump.js`'s `pool.pump_start`/`pool.pump_stop`
  events gained a `reason` field, so `events.db` can distinguish a solar-driven start/stop from a
  scheduled or override-driven one (issue #587). Merged 2026-09-05 as PR #595, commit `f380a143`.
- **Why it matters that this is unproven**: this is precisely the kind of change #401 exists to
  catch — #540, #530, #547, #550 were all changes that passed `make test` and code review and then
  failed (or silently no-op'd) on real hardware, because the goja emulator models neither firmware
  key limits nor interpreter stack depth (#496).
- **What is known instead of a hardware run — a byte proxy, not a heap figure**: pre-deployment
  baseline read over HTTP from `filtration-hiver` 2026-09-05 09:58 CEST — `mem_free` **7280**,
  `mem_peak` **22918**, script id 2 running. Minified size grew **36038 → 36674 bytes (+636)** from
  the change. **No `mem_used`/`mem_free` reading has been taken with this build actually running.**
  A minified-byte delta is not a heap measurement — see the #570/#569 "heap calibration" finding
  recorded elsewhere in this campaign (source bytes and resident heap cost do not correlate
  linearly; deletions have measured as low as 15% of their byte delta in resident cost, and single-
  call-function inlining has measured as high as 167%). **Do not infer this change's resident cost
  from +636 bytes.**
- **What would close this entry**: a `Script.GetStatus` reading against a device actually running
  the post-#587 build (heap after ~30s settle, per the "only settled readings are measurements" rule
  from #594), and at least one real `pool.pump_start`/`pool.pump_stop` event observed in `events.db`
  with a non-empty `reason` field.

---

## Sourcing note for this refresh (2026-09-06)

Test cases #2–#5 above were mined from campaign #401's own issue-comment history (issues #401, #540,
#559, #587, #594) rather than re-run in this session — this refresh's own device interaction was
limited to the read-only `curl` checks noted under each device's availability section. Every figure
above is either dated-and-sourced to a specific issue comment, or explicitly marked as not yet
measured. No figure from the original PR #427 content, or from campaign #401, was carried forward
without a citation.
