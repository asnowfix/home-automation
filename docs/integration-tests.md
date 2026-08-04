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
- It already runs two permanent scripts that must never be stopped or deleted by a test case:
  script id 1 `watchdog.js`, script id 2 `myhome-link.js`. Test scripts under evaluation should
  land at script id 3+ (whatever the device assigns next).

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
- **macOS netcat gotcha**: `nc -u -l <port>` (without `-k`) stops accepting data after the first UDP
  "session" ends, silently going quiet. Always use `nc -u -l -k <port>` for a debug capture that
  must keep accumulating across a device reboot/restart.
- **Script upload takes a bare embedded name, not a path**: `ctl shelly script upload <device>
  <script-name>` reads only from `internal/shelly/scripts/scripts.go`'s `//go:embed *.js` — it does
  **not** accept a filesystem path. Passing a path produces the misleading `Failed to read script
  <path>: file does not exist` (that's `io/fs` wording, not "no such file on disk"). To upload a
  specific version, checkout/edit that exact file in `internal/shelly/scripts/` and rebuild the
  binary (`make generate && make build`) — there is no way to point the uploader at an arbitrary
  file. Use `--force` to re-upload when the on-device KVS version hash already matches the content
  you're pushing (it will, on a repeat upload of unchanged content).

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
  nc -u -l -k 5099 > development-debug.log &

  ./myhome/myhome ctl shelly call development Sys.SetConfig \
    '{"config":{"debug":{"mqtt":{"enable":false},"websocket":{"enable":false},"udp":{"addr":"192.168.1.88:5099","level":4}}}}' \
    --mqtt-broker tcp://192.168.1.2:1883

  # Verify it took, and reboot ONLY if restart_required was true in the response above:
  ./myhome/myhome ctl shelly call development Sys.GetConfig --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly reboot development --mqtt-broker tcp://192.168.1.2:1883   # only if needed

  # 2. Upload pool-pump.js from the exact commit under test (checkout that commit / ensure
  #    internal/shelly/scripts/pool-pump.js matches it, then `make generate && make build` first).
  ./myhome/myhome ctl shelly script upload development pool-pump.js \
    --mqtt-broker tcp://192.168.1.2:1883
  # (note the assigned script id, e.g. 3, from the "id: N" in the success message)

  # 3. Configure the one required KVS key (use the device's own ID and the id from step 2):
  ./myhome/myhome ctl shelly kvs set development script/pool-pump/preferred \
    shellyplus1-08b61fd98f44 --mqtt-broker tcp://192.168.1.2:1883

  # 4. Create short-interval test Schedule jobs (id substitutes the script id from step 2):
  ./myhome/myhome ctl shelly call development Schedule.Create \
    '{"enable":true,"timespec":"0 0,6,12,18,24,30,36,42,48,54 * * * SUN,MON,TUE,WED,THU,FRI,SAT","calls":[{"method":"script.eval","params":{"id":3,"code":"handleMorningStart()"}}]}' \
    --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly call development Schedule.Create \
    '{"enable":true,"timespec":"0 3,9,15,21,27,33,39,45,51,57 * * * SUN,MON,TUE,WED,THU,FRI,SAT","calls":[{"method":"script.eval","params":{"id":3,"code":"handleEveningStop()"}}]}' \
    --mqtt-broker tcp://192.168.1.2:1883

  # 5. Start the script and watch it:
  ./myhome/myhome ctl shelly script start development pool-pump.js --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly call development Script.GetStatus '{"id":3}' --mqtt-broker tcp://192.168.1.2:1883

  # 6. Per #421's repro recipe, repeat stop/start in a tight loop and watch for
  #    `running: false` + `error_msg` containing "Too many calls in progress":
  ./myhome/myhome ctl shelly script stop development pool-pump.js --mqtt-broker tcp://192.168.1.2:1883
  ./myhome/myhome ctl shelly script start development pool-pump.js --mqtt-broker tcp://192.168.1.2:1883
  # ... repeat, checking Script.GetStatus and the debug log after each cycle ...
  ```

- **Pass/fail criteria**:
  - **Fail (bug reproduced)**: `Script.GetStatus` for the pool-pump script returns `running: false`
    with `error_msg` containing `Too many calls in progress`, matching #421's reported signature.
  - **Pass (fix verified, for use once #421's fix lands)**: the script survives boot and repeated
    stop/start cycles without that crash; queued KVS writes (`storeValue` mirrors of
    `active-output`, `schedule-mode`, `runtime-sec`, `turnover-today`) all land correctly afterward
    (spot-check via `ctl shelly kvs get development "script/pool-pump/*"`); a restart mid-window
    self-corrects per Bug B's fix (separate assertion, not exercised by this run).
  - **Inconclusive / blocked**: script fails to even start for a reason unrelated to Bug A (see
    Result below — this is what actually happened on this run).

- **Originating issue**: #421 (crash), test-campaign process defined by #422.

- **Last run**: 2026-08-04, this session (branch `test/422-live-rig-421-repro`, worktree
  `.claude/worktrees/agent-a831607e74326abe0`, base commit `ae4c5da`). **Result: BLOCKED — could
  not reach Bug A's code path at all.** Full detail below.

### Result — #421's crash was NOT reproduced; a different, earlier-blocking crash was found instead

`pool-pump.js` (current `main`, `ae4c5da`, includes #405's solar hysteresis) crashes with
**`out_of_memory`** the instant it is started on `development` — before executing a single line of
its own code, let alone reaching the KVS-write-overlap code paths #421 describes.

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

**Conclusion**: this Plus 1 cannot currently run current-`main` `pool-pump.js` at all, given its
existing script load (`watchdog.js` + `myhome-link.js`), because the script's minified size
(39 KB) alone exceeds the device's ~21 KB of free shared JS heap. This is a **hardware/firmware
resource-budget gap**, not the #421 bug — it blocks reaching Bug A's crash site entirely, so this
run cannot confirm or deny #421 either way. #422's assumption that "device-type auto-detection is
purely by switch count... so pump scripts under test need no code changes to run on it" was **not
verified this session** — the script never ran long enough to reach `loadConfig`'s device-type
detection (`pool-pump.js:321-331`) at all, since the crash happens during compilation, before
`init()` runs. The claim may well be correct once the script can actually boot, but this run
produced no evidence either way. What this run does show is that #422's premise does **not** hold
at the *memory-budget* level: a Shelly Plus 1 (this device's hardware family) very likely has
meaningfully less JS-script
heap than a Shelly Pro1/Pro3 (the family `filtration-hiver` and the mesh's Pro3 controller belong
to) — Pro-series Gen2 devices use a more capable MCU (commonly with PSRAM) than Plus-series. This
was not independently confirmed against Shelly's hardware specs in this session (no internet-backed
verification performed), but is consistent with all evidence gathered above and with the existing
`reference_shelly_pro3_limits.md` note. **Recommendation for a future session**: either (a) test
against a spare Pro-family device instead of this Plus 1 for any `pool-pump.js` version at or
above this size, or (b) treat the script's own on-device footprint (currently 39 KB minified) as a
resource budget that needs active management (comments already show #402/#405 authors being memory
-conscious — e.g. clearing `CONFIG_SCHEMA`/`COMPONENT_NAMES` after use — but that happens **after**
the compile step that's actually failing here, so it can't help with this specific ceiling).

**Device end state after this test run** (left in this state deliberately, for the next session to
pick up): `development` has script id 3 = `pool-pump.js` (current `main`, `ae4c5da`) uploaded but
**not running** (crashed via `out_of_memory` on every start attempt), KVS
`script/pool-pump/preferred` = `shellyplus1-08b61fd98f44`, two test `Schedule` jobs (ids 2 and 3,
inert while the script can't run), physical switch confirmed **off** (`Switch.GetStatus {"id":0}`
→ `"output": false`), `watchdog.js`/`myhome-link.js` untouched and still running. Device-level UDP
debug remains enabled, targeting `192.168.1.88:5099` at `level: 4` — see "Debug capture" below for
whether that listener is still live.

### Debug capture (raw UDP dump, continuous)

- **Listener**: `nc -u -l -k 5099`, started at 2026-08-04 08:17:39 local time, PID `14982` on the
  dev Mac used for this session. `-k` is required (see the netcat gotcha above) — plain `nc -u -l`
  goes silent after the first UDP datagram burst.
- **Log file**: `/Users/fix/tmp-claude/claude-501/-Users-fix-Desktop-GIT-home-automation--claude-worktrees-bugfix-pool-pump-not-starting/50548ab1-7478-4d25-8017-440e5aaba035/scratchpad/live-logs/development-udp-raw-20260804-081739.log`
  — raw NUL-separated device debug records (device firmware format:
  `<device> <msgCount> <timestamp> <component> <level>|<message>`, records separated by `\x00`).
  Pipe through `tr '\0' '\n'` to read line-by-line.
- **Verified working**: captured live `Sys.SetConfig`/reboot/MQTT-reconnect traffic during this
  session (visible in the log around the `Shelly.Reboot` call). Captured **zero** bytes across 3
  `pool-pump.js` start attempts, which is itself the evidence that the crash happens pre-execution
  (see Result above) — not a capture-pipeline failure (the pipeline demonstrably works for every
  other event on the device).
- **As of this doc's last edit, the capture is still running** — left running intentionally per
  this campaign's session instructions, so a follow-up session can keep appending to the same file
  without re-establishing the listener. Stop it with `kill 14982` (or find the current PID with
  `pgrep -f 'nc -u -l -k 5099'` if the session has moved on) once no longer needed.
- Device-side config: `Sys.GetConfig` → `debug.udp.addr = "192.168.1.88:5099"`, `level: 4`. If the
  dev Mac's LAN IP changes (check `ifconfig en0`) or the VPN state changes, re-verify this address
  still matches before trusting a "no output" result — a stale address would silently produce the
  same zero-bytes symptom as a genuine pre-execution crash, so cross-check `Script.GetStatus`
  (`errors: ["out_of_memory"]`, `mem_free`) for independent confirmation the way this session did.
