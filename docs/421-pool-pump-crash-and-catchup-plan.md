# Plan: issue #421 — pool-pump "Too many calls in progress" crash + restart catch-up

Tracks implementation of https://github.com/asnowfix/home-automation/issues/421.
Two independent bugs in `internal/shelly/scripts/pool-pump.js`, plus the minimal
emulator capability (a slice of #250) needed to reproduce Bug A in
`pkg/shelly/script/run.go`.

Method: TDD / Prove-It. Emulator capability first, then failing tests proving
both bugs reproduce against current `main`, only then the fixes.

## Phase 1 — Emulator capability (prerequisite for reproducing Bug A)

- [x] `pkg/shelly/script/device_state.go`: `ExecutionMode` type
      (`DeviceTestMode` zero value / `DeviceExtensionMode`), `MaxConcurrentCalls`
      const, `DeviceState.Mode` and `DeviceState.CallDelay` fields.
- [x] `pkg/shelly/script/run.go`: per-VM `callsInFlight` counter around the
      `Shelly.call` dispatch closure. `DeviceTestMode` panics with a message
      matching the real device's `Too many calls in progress` once 5 calls are
      already in flight. `CallDelay` (only meaningful in `DeviceTestMode`)
      defers the *release* of a call's in-flight slot rather than the call's
      dispatch/callback — this lets tests create realistic overlapping
      in-flight calls without disturbing any script's existing synchronous
      callback-ordering assumptions. Inert (zero overhead, zero behavior
      change) in `DeviceExtensionMode` and whenever `CallDelay == 0`.
- [x] `pkg/shelly/script/run.go`: `RunWithDeviceState`'s event loop now treats
      *any* handler `Handle()` error as a full script crash (stops the loop,
      returns the error) instead of logging and continuing. This is required
      infrastructure, not scope creep: on real Shelly firmware an uncaught
      exception in *any* callback (timer tick, event handler, MQTT message)
      kills the whole script, and Bug A's real crash happens inside a
      `Timer`-driven task-queue tick, not during the initial synchronous
      script evaluation — the emulator can't observe "the script crashed"
      for that path without this fix.
- [x] Do NOT implement the rest of #250 (timer/event-handler/status-handler/
      MQTT-subscription count ceilings) — only the concurrent-call slice
      needed here. Cross-referenced in code comments.

## Phase 2 — Bug A: reproduce, then fix

- [x] `internal/shelly/scripts/pool_pump_test.go`: new test drives a normal
      boot (reuses the existing `TestPoolPump_InitVerifiesSchedules` fixture
      shape) with `Mode: DeviceTestMode` (default) and a `CallDelay` long
      enough that config-loading's sequential `KVS.Get` calls (one dispatched
      per 200ms task-queue tick, ~14 of them) overlap past the 5-concurrent
      ceiling before the fix exists. Confirm it fails against current `main`
      with a "Too many calls in progress" error — report the exact output.
- [x] Fix (`pool-pump.js`): a single choke point (`Shelly.call` is reassigned
      once, near `TASK_QUEUE`, to wrap every callback via `trackCall`) tracks
      real script-wide `CALLS_IN_FLIGHT`. `processTaskQueue` throttles its own
      dispatch to `MAX_CALLS_IN_FLIGHT` (4, one below the real ceiling),
      retrying the same task on the next tick instead of assuming 200ms was
      "long enough". This is the root-cause fix — no call site needed manual
      edits.
- [x] Second, independent layer of defense: `task()` in `processTaskQueue` is
      wrapped in `try/catch` — a queued task throwing no longer kills the
      whole script.
- [x] Re-run the new test — now passes (init completes, no crash).

## Phase 3 — Bug B: reproduce, then fix

- [x] New test: boot with a schedule window (`Schedule.List` fixture) that
      excludes "now" and the switch already "on" in `ComponentStatus`. Assert
      the fixed `enforceOutputState()` path calls `doStop()` and
      `active-output` ends up `-1` without waiting for any schedule tick
      (there is no cron emulation in this harness).
- [x] Fix (`pool-pump.js`): new `restartCatchUp()`, queued right after the
      existing init steps complete (after `verifySchedules`, so it reads a
      validated `Schedule.List`), compares current time-of-day against the
      currently active mode's window (mode-aware: summer →
      handleMorningStart/handleEveningStop, winter →
      handleNightStart/handleNightStop) and calls `doStart()`/`doStop()` if
      the physical state disagrees. Skips gracefully (no action) when the
      window can't be resolved (e.g. still-symbolic `@sunrise` timespecs on a
      schedule that has never had `updateScheduleMode()` run against it).
- [x] Regression guard: three existing tests intentionally leave the pump
      "on" in `ComponentStatus` at boot to test *other* behavior (water-supply
      protection, runtime-accounting continuity). Bug B's fix would otherwise
      immediately stop them, since their shared schedule fixture's night
      window (23:15–00:15) essentially never contains real test-run wall-clock
      time. Added `activeNightWindowSchedules()` (widens that window to
      bracket `time.Now()`) and pointed those three tests at it.

## Phase 4 — Full regression pass

- [x] Re-run all of `pool_pump_test.go`.
- [x] `make test` (canonical, root + all workspace sub-modules).
