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
