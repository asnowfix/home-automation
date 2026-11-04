# pool-pump.js winter crash: goja stack-limit experiment (#496 / #661)

Earlier investigation notes live in #661's comment of 2026-11-03 21:50 UTC. This file is the
append-only log of the goja call-stack-limit experiment (branch `exp/496-goja-stack-limit`,
started 2026-11-04 09:26 CET). Newest at the bottom. No device is touched.

## 09:45 step 1: emulator knob

- `DeviceState.MaxCallStackSize` (0 = unlimited, default) -> `vm.SetMaxCallStackSize(n)` in
  `createShellyRuntime`. First knob of #250's device-test mode; nothing else of #250 built.
- All `goja.AssertFunction` call sites in run.go now go through `assertFunction`
  (stack_limit.go), which records `*goja.StackOverflowError` in a process-global counter,
  `script.StackOverflows()` / `ResetStackOverflows()`.
- Fact (a): a goja stack overflow is NOT catchable by JS try/catch. It aborts the whole
  RunScript/callable with a Go error (`TestMaxCallStackSizeTripsAndIsNotCatchable`: the
  statement after the try/catch is never reached, RunScript fails). So on goja the
  `processTaskQueue` try/catch cannot absorb an overflow: an overflow inside a task aborts
  that callback invocation only, not the VM.
- Fact (b): an overflow inside a Go-invoked callback (Shelly.call completion) is returned by the
  callable to the emulator, which discards it at most sites: the run keeps going, nothing is
  logged (`TestMaxCallStackSizeOverflowInCallbackIsRecorded`). Hence the counter; a
  measurement must assert `StackOverflows()==0`, otherwise "survives" is meaningless.
- Unit tests in `pkg/shelly/script/stack_limit_test.go` pass.

## 09:57 step 2: stack-depth sweep (negative result)

Test: `TestPoolPump_StackDepthSweep` (internal/shelly/scripts/pool_pump_stack_limit_test.go),
opt-in: `POOLPUMP_STACK_SWEEP=1` (optionally `POOLPUMP_STACK_ONLY=<substring>`). Pro1, winter mode,
logging on, forecast server (winter) so the daily-check forecast parse runs after init, night
window = now -30/+30 min (inside) or +120/+180 min (outside), runtime record (and run-start when
relay ON) dated today or yesterday. A run "survives" at limit L when: schedule-mode KVS written
AND `pool.run_window` emitted (init + Schedule.List parse + forecast parse done) AND
`script.StackOverflows()==0` AND a forced rollover checkpoint
(`STATE.runtimeTs=<yesterday>; runtimeFlushFire()`, injected) AND an injected
`F_WIN_*=<away from now>; handleNightStop()` turns the relay OFF. Each scenario first runs
unlimited (harness sanity), then a binary search over [1,40], then confirms min-1 fails.
Wall clock: 772.6 s for 9 scenarios, 259.0 s for the 3 midnight ones.

| scenario (switch:0 at start / window contains now / record date) | min surviving limit | delta vs OFF/outside/today | phase that fails at min-1 |
|---|---|---|---|
| OFF / outside / today (baseline) | 11 | 0 | init |
| OFF / outside / yesterday | 11 | 0 | init |
| OFF / inside / today | 14 | +3 | init |
| OFF / inside / yesterday | 14 | +3 | init |
| ON / outside / today | 13 | +2 | init |
| ON / outside / yesterday | 13 | +2 | init |
| ON / inside / today | 13 | +2 | injected handleNightStop() eval only (init and flush survive at 12) |
| ON / inside / yesterday (production failure shape) | 13 | +2 | handleNightStop() eval only |
| ON / inside / yesterday, logging off | 13 | +2 | handleNightStop() eval only |
| @00:01 (clock shifted) OFF / outside / today | 11 | 0 | init |
| @00:01 OFF / inside, window wraps midnight / yesterday | 14 | +3 | init |
| @00:01 ON / inside, window wraps midnight / yesterday | 13 | +2 | handleNightStop() eval only |

Reading: relay-ON-in-window needs 13, relay-OFF-in-window needs 14 (starting the pump from the
window is the deepest path: init -> onWindowJobs -> reconcile -> Switch.Set chain). Relay ON in
window is therefore NOT deeper than the scenarios that completed in production. The
yesterday/today record date, the midnight wrap, and logging make no difference at all. The
deepest single entry point for ON/inside is the injected eval handleNightStop() (13); init and
the rollover flush need <= 12. Earlier uncommitted result (11 vs 10 both ways) is consistent.

Caveat: goja counts JS call frames; Espruino is bounded by C stack consumed by expression
nesting (#496). The numbers say the call-frame structure of the ON-in-window path is not
special; they say nothing about argument-position nesting, heap (the other candidate in #661),
or a lost Shelly.call callback (hypothesis 2). Judgement: this evidence is much weaker than
it looks, and a null result here cannot exonerate stack on the device; it only fails to
support it.
Ruled out in the emulator: day-rollover state, relay state, window wrap, logging, daily-check
forecast parse concurrent with queued runtime closures, as separators by call-frame depth.
Not modelled: heap, nesting depth inside one expression, firmware callback loss.
