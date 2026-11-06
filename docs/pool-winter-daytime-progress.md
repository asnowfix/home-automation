# Pool pump runs in daytime while in winter mode — plan and progress (#621)

Tracking issue: #621 ("pool pump: solar events start filtration in winter (night) mode").
Branch: `worktree-fix+pool-always-start-as-in-summer` (based on `origin/main` @ `ccc92232`).

## Plan

1. Root-cause from `events.db` and the script (done, see below).
2. Gate solar-driven *starts* on summer mode in `internal/shelly/scripts/pool-pump.js`
   (acceptance criteria of #621). Keep the hard-ceiling stop and water-supply priority as they are.
3. Red/green regression test(s) in `internal/shelly/scripts/pool_pump_test.go`; adapt existing
   solar tests that only passed because they ran in the default winter mode.
4. Commit, then full `make test` in the background.
5. Back-port: the branch is identical to `origin/main` at `ccc92232`, so the same commit goes onto a
   new branch from `origin/main` in its own worktree.

## Progress log

### 2026-10-31 — root cause (coordinator, read-only)

Measured on a copy of the NAS `events.db` taken 2026-10-31 17:33 (events 2026-08-02 .. 2026-10-31),
device `shellypro1-ec62608c0230`. Query script:
`/tmp/claude-1004/-home-fix-GIT-home-automation/d255c877-7b5b-4cfd-9daa-3eebece46eae/scratchpad/q621.py`.

- Reason codes (`pool-pump.js`, `REASON_*`): 0 water-supply interlock, 1 button, 2 other override,
  **3 solar**, 4 run window.
- **Every daytime start on a winter-mode day carries reason 3 (solar).** No daytime start on a
  winter day carries reason 4, so the summer schedule is *not* left enabled; the night window
  (23:15 -> 00:15, reason 4) runs as intended every night.
- Cause in code: `desiredOutput()` checks `F_SOLAR_WANT` before the run window and returns ON with
  no reference to `STATE.scheduleMode`; `checkSolarHysteresis()` never reads the mode either. Solar
  is "additive" in both modes, which contradicts the winter intent stated in #621.
- **Observed rate:** since winter mode first appeared (2026-10-09), 11 days ended in winter mode;
  **6 of them** (Oct 9, 25, 26, 27, 30, 31) had at least one solar start while in winter mode —
  **9 solar starts, 21.3 h of daytime pumping** in total. The winter days without one (Oct 20,
  22, 23, 24, 28) presumably had no solar surplus over 500 W for 5 min — inference, solar
  availability itself is not in `events.db`.
- The reason-0 stops around 11:00 (Oct 27, 30, 31) are `pool.water_supply_protected` interlock
  episodes (15–25 min, daily, as already characterised in #523), followed by
  `pool.water_supply_restored` and an immediate solar restart (solar accumulates through a resupply,
  `docs/pool-pump.md` "Solar accumulates *through* a resupply"). Not a defect; they vanish with the fix
  because no solar run is in progress in winter.
- Oct 21 and Oct 29 were genuine summer days (forecast 21.7 C / 20.2 C > 20 C threshold); their
  daytime runs are the summer window plus solar, as designed.
- "Starts as in summer" hypothesis: ruled out. No code path defaults to summer; with no saved mode
  the script defaults to winter (`pool-pump.js` ~line 1154). The daytime runs look like summer
  behaviour only because solar fires in daylight.

### 2026-10-31 — implementation (developer)

- Tests first (commit 6bc01774): added `TestPoolPump_SolarNeverStartsPumpInWinter` and helper
  `solarSummerStorage()` (Storage `schedule-mode: summer`). Adapted to boot in summer so they keep
  asserting what they did: SolarStartsAndStopsPump, SolarStartAndStopSurvivesOwnEcho,
  SolarRespectsHardCeiling, WaterSupplyOverridesSolar, SolarHardCeiling_DayRolloverUnblocksWithoutRestart
  (these need a solar START; the last three assert "stays off" and would otherwise pass vacuously in
  winter), SolarStaleFallsBack (button), SolarStaleTsFallsBackImmediately (stays-off, vacuous otherwise).
  RuntimeRecovery tests already seed summer and never start solar: unchanged.
- RED against the unfixed script (19.7 s run):
  `--- FAIL: TestPoolPump_SolarNeverStartsPumpInWinter (7.21s)`
  `pool_pump_test.go:1842: winter mode: solar must not start the pump, got active-output=0`
- Design: one-line gate in `checkSolarHysteresis()`: `STATE.scheduleMode !== 'summer'` joins the
  stale/absent condition (reset hold timers, `solarWant(false)`, return). F_SOLAR_WANT has a single
  writer (`solarWant()`, only called from there), so gating that function means no ON verdict can
  come from solar in winter AND a latched run is released at the next solar message or 30 s tick,
  with no new function/state (heap). `desiredOutput()` untouched: hard-ceiling stop and water
  interlock keep their order in both modes. A winter stop goes through `solarWant(false)` ->
  reconcile("solar gone"); desiredOutput() then falls to the window branch, so the stop carries
  whatever branch decides (window/ceiling), the same as a summer "solar gone" stop; RC_ACTIVE_REASON
  stays solar for the run's own stop label. Mode change while latched is covered by
  `TestPoolPump_SolarRunStopsWhenModeTurnsWinter` (added).
- Ruled out: gating in desiredOutput() (would leave F_SOLAR_WANT latched and the hysteresis
  timers running, and costs a second check on every reconcile); gating both (redundant).
