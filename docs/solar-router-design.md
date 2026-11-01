# Solar router: architecture and design proposal

Status: **proposal for review** (epic #652). Not implemented. Nothing here has been run against hardware.
Inputs: `docs/solar-router-research.md` (prior art, local baseline) and `docs/mater-france-research.md` (Mater France, Linky TIC). Facts about the repo come from those notes; this document does not re-derive them.
Written: 2026-11-01.

## 1. Problem and goals

Distribute the solar power the house produces across several consumers so that as little as possible is injected into the grid. **Injected power earns nothing**, so the target is a grid exchange near 0 W (slightly on the import side), not maximum revenue.

Goals:
1. Several production providers behind one abstraction (Beem today; Mater France panels coming; others later).
2. Several consumers with different behaviours (pool pump, water heater, office heaters, dishwasher, later washing machine).
3. Works offline: no feature may need the internet to keep the house safe and running.
4. Each Shelly keeps its core function when the daemon is down.
5. Small and explainable. This is a hobby project that also teaches Go; prefer a small allocator over a framework.

Non-goals (for now): battery management (none installed), selling surplus, three-phase handling, a forecast-driven optimiser (deferred, see section 9).

Typical numbers: about 1 kW surplus in summer today, roughly double once the Mater panels are installed; 1.5-2 kW in winter. A 300 L resistive water heater is usually 1.5-3 kW (nameplate unknown), so **the surplus is smaller than or comparable to the biggest load**. That drives most of the design below.

## 2. What exists and what is missing

Exists (verified in the research note): a daemon `SolarAggregator` publishing a retained production sum on `myhome/energy/solar/available`; `pool-pump.js` with threshold hysteresis, stale-signal fallback to its schedule, and a summer/winter schedule mode; a static claimer registry that does no arbitration.

Missing: any grid signal, any notion of "what is each load drawing now", any priority or reservation logic, any second consumer.

## 3. Central idea: measure at the grid point

The research shows the community converges on one pattern: **regulate the power at the grid connection point toward a setpoint**. For a zero-export goal that gives a very simple formula.

Let `grid_net = import - export` (positive means drawing from the grid). Let `P_ctrl` be the power currently drawn by the loads the router controls. Then

```
available_w = -grid_net + P_ctrl - residual
```

`residual` is a small safety margin (default 0 to 50 W, aimed at slight import). Reading: the power the router could hand to controlled loads is whatever is exported now, plus what the controlled loads already absorb, minus the margin. This is evcc's site-surplus formula specialised to one meter.

Consequences:
- **Production data is not required for the control loop.** Beem or Deye production is useful for sanity checks (is the sun up, is the grid reading plausible), staleness detection and dashboards, but the loop closes on the grid meter. This is why the grid signal is phase 1 and the provider work (phase 2) is not on the critical path.
- **`P_ctrl` must be known per load.** Either metered (a Shelly with power metering on the load's circuit) or taken from a configured nominal power while the load is on. Unmetered nominal power is acceptable for loads whose power is stable (heater), poor for variable ones (pump at variable speed, which reports its own power).
- **With zero export you only see export while there is headroom.** When loads absorb everything, `grid_net` is about 0 and `available_w` equals `P_ctrl`. The allocator must therefore probe upward deliberately (turn on the next load, see if import appears) and back off on import. This is the classical behaviour of all reviewed routers and is why enable/disable delays and thresholds matter.

### 3.1 Grid signal: Linky TIC

The only grid meter is the EDF Linky. Its TIC output in **standard mode** exposes `SINSTS` (instantaneous apparent power drawn, VA) and `SINSTI` (injected), recomputed every second per the Enedis specification (cited in the Mater France note). Plan: an ESP-based Wi-Fi TIC reader publishing to the existing MQTT broker; a small Go parser in the daemon (no Go standard-mode reader was found, so we write one; the frame format is simple and documented).

Caveats, all to be resolved in phase 1 by measurement:
- Values are **apparent** power (VA), not active (W). With resistive and mostly resistive loads the difference is small; calibrate against a clamp meter once.
- Standard mode must be enabled by the supplier and can revert after a meter update.
- It is unknown whether `SINSTI` is reported on a no-sale installation. If not, export is invisible and the loop would have to infer it as `pv - consumption`, which needs a production provider. This is the main technical risk of the design; phase 1 exists to answer it cheaply and read-only.
- A Shelly clamp meter (Pro EM / 3EM) on the incoming line is the fallback grid signal. The research found no sign convention in the Shelly documentation, so direction must be measured before use.

## 4. Architecture

```
 producers                      grid signal                loads
 Beem (cloud)                   Linky TIC reader (MQTT)    pool pump (Pro3/Pro1)
 Mater/Deye (Solarman V5)       [fallback: Shelly EM]      office heaters (via rooms)
        |                              |                    water heater (contactor)
        v                              v                    dishwasher / washer
   Provider interface           Meter interface                  ^
        \                            /                           | grant (W) per load
         v                          v                            |
              Site model (roles: pv, grid, load meters)
                           |
                     surplus estimator  (filter, staleness)
                           |
                        Allocator  --> retained MQTT topic per load
                                       myhome/energy/grant/<load>
```

Layering follows the repo's three-tier rule: pure logic in `internal/myhome/energy/` (site model, estimator, allocator; no MQTT, no I/O, table-testable), providers and the grid reader in `myhome/daemon/` (wiring) and `pkg/` (generic clients such as `pkg/beem`), device scripts in `internal/shelly/scripts/`. No business logic in `myhome/ctl/`.

### 4.1 Roles instead of an untyped reading

Today `SolarReading` has `Source`, `Watts`, `TS` and no meaning for the sign. Replace the single `SolarSource` with role-typed readings, borrowing evcc's explicit roles (declared in configuration, never inferred):

```go
type Role int // RolePV, RoleGrid, RoleLoad

type Reading struct {
    Source string    // "beem", "linky", "pool-pump", ...
    Role   Role
    Watts  float64   // RolePV: >= 0 produced. RoleGrid: + import / - export. RoleLoad: >= 0 drawn.
    TS     time.Time // source timestamp, not receipt time (staleness is judged on this)
}
```

A provider is anything that emits `Reading`s for a role. Beem emits `RolePV`; the Linky reader emits `RoleGrid`; a metered load emits `RoleLoad`. Adding Mater/Deye is one more `RolePV` emitter. Sign conventions are fixed in this one place and converted at the edge.

### 4.2 Loads

```go
type Load interface {
    Name() string
    Kind() Kind              // Comfort, Throttleable, ThermalStorage, Deferrable
    NominalW() float64
    MinW() float64           // smallest grant worth acting on (0 for on/off => NominalW)
    Eligible(now time.Time) bool   // e.g. someone home and room cold; summer mode; window open
    Draw() (float64, bool)   // current power if known
}
```

Load kinds:

| Kind | Examples | Behaviour |
|---|---|---|
| Comfort | office heaters | Eligible only if the room is occupied and below its setpoint; high priority. Uses the room model (phase 4). |
| ThermalStorage | water heater | On/off by contactor; runs only while its own thermostat calls for heat; needs a surplus at least near its nominal power or a long averaging window (section 5.3). |
| Throttleable | pool pump | Absorbs partial surplus (variable speed on the Pro3); summer mode only at first. |
| Deferrable | dishwasher, washer | Started once inside a window when surplus or forecast is sufficient; cannot be throttled or interrupted. |

### 4.3 Where decisions run

**The daemon decides; each device decides last.** The allocator (daemon) computes a *grant* in watts per load and publishes it as a retained, timestamped message. The device script applies its own local hysteresis and safety on the grant, exactly as `pool-pump.js` does today with the aggregate. If the grant is stale or absent, the device falls back to its own schedule (daemon-optional rule). This keeps cross-device arbitration in one place without making any device depend on the daemon for its core function.

Rejected alternative: all decisions on Shelly devices. Cross-load priority would need device-to-device messaging within about 7 KB of free heap per script; not viable.

### 4.4 Allocator policy

Borrowed from evcc's prioritiser (design only; evcc is not imported, see section 8): **strict priority with reservation**. Loads are ordered; each higher-priority load that is eligible and *starting* reserves the surplus it needs, so a lower-priority load does not grab it. Within a priority, no proportional split in v1. Proportional sharing is deferred until phase 1 and 3 data show it is needed (decision for the owner, open question 5).

Default order (configurable, and **seasonal**, decided by the owner 2026-11-01). The order is data, not code, and follows the pool pump's existing `schedule-mode` (summer/winter):

- **Summer:** 1) safety minimums (pool minimum filtration, water-heater minimum), 2) office heaters, 3) **pool pump**, 4) **water heater**, 5) dishwasher, washer.
- **Winter:** 1) safety minimums, 2) office heaters, 3) **water heater**, 4) dishwasher, washer. The pool pump is not a solar load in winter: it runs only in its night window and solar starts are gated off in winter mode (#621 / PR #651), so it has no rank to hold.

Where the office heaters sit relative to the pool pump in summer (heaters first, as drafted) is still worth confirming in review.

## 5. Control details

### 5.1 Smoothing and debounce

The measured pool-pump failure (360/420 W oscillation, hold timer reset by single samples) is the baseline problem. Rules for the estimator:
- Judge staleness on source timestamps; drop a role's reading after `solar.stale_after` (existing option, default 5 min). Grid readings should use a much shorter limit (about 10 s) since they are 1 s data.
- Filter `available_w` with a short moving median followed by an exponential average. Exact windows are set from phase 1 measurements, not guessed.
- Enable and disable use **separate thresholds and separate delays**, and a delay timer is reset only when the filtered value stays on the wrong side for a configurable grace period, not on one sample. (The evcc summary did not establish whether it resets on one sample, so this is our own decision, not a borrowed behaviour.)

### 5.2 Minimum on/off and anti-cycling

Every load declares minimum on and off times (pump, contactor, appliance). The allocator never commands a transition inside them. The pool pump's existing anti-cycling fuse and water-supply protection stay authoritative and cannot be bypassed by a grant.

### 5.3 The 1-2 kW surplus against a 2-3 kW water heater

With an on/off contactor larger than the instantaneous surplus, a naive loop flaps or imports. Options, to decide from phase 5 measurements:
1. **Burst-fire SSR or dimmer** (proportional): matches the surplus, as the reviewed community routers do; needs hardware and a heatsink.
2. **Long-window contactor control**: switch on only when the averaged surplus over a long window exceeds a fraction of nominal power, accept some import, rely on the tank's thermal inertia. Cheap, imperfect.
3. **Run it only after the pool pump** has absorbed what it can, or only at off-peak (HC) rates when the sun is insufficient.

The metered Shelly on the contactor circuit is needed regardless: it measures the real nameplate power, and a closed contactor drawing about 0 W means the thermostat is satisfied (the tank is full), which the router cannot otherwise know.

Legionella and minimum-temperature requirements are not established by the research; the existing thermostat plus the off-peak (HC) path stays as the safety floor until that is resolved.

## 6. Failure modes and degraded operation

| Failure | Behaviour |
|---|---|
| Daemon down | Grants go stale; every load falls back to its own schedule or thermostat. No device depends on the daemon for its core function. |
| MQTT broker down | Same as daemon down for devices; the daemon's grid reader also loses its signal. |
| Internet down | Local loop unaffected (Linky reader, local loads). Beem (cloud) and any forecast fetch time out and are treated as absent. Mater/Deye local polling is unaffected if port 8899 works. |
| Linky reader down | Grid signal stale after about 10 s; allocator grants zero; loads revert to their fallback. Never extrapolate a stale grid reading. |
| Provider down | Production becomes unknown; the loop still runs on the grid meter. Production is used for sanity checks only. |
| Reading implausible | Out-of-range or sign-flipped readings are dropped and counted (events); no grant is issued from a single bad sample. |

## 7. Observability and evidence

Every transition (grant change, load start/stop, signal stale/restored, clamp of implausible input) emits an event into `events.db`, because the project rule is that any mitigation must state the observed failure rate. Phase 1 publishes the computed surplus read-only and runs for at least a week before any load is driven, so phase 3 starts from measured signal quality (noise, cadence, plausibility) and not from assumptions.

## 8. Reuse and licence

The repository is MPL-2.0. Decisions, subject to owner confirmation:
- **evcc (MIT, Go):** borrow concepts (site formula, separate enable/disable delays, priority with reservation); do not import the module (monolithic app, EV-shaped charger types, some sponsor-gated components). Porting small MIT snippets is allowed with the copyright notice kept; the Deye register map from evcc's template is a likely candidate for phase 2.
- **GPL or non-commercial projects** (HACF Solar-Router, xlyric/APPER, openhems): read for ideas only; copy no code.
- **Unlicensed projects** (some Shelly-dimmer gists, Hypfer's Deye reader): ideas only.
- Changing the repo licence is not needed and not proposed.

## 9. Deferred

- Forecast-aware optimisation (EMHASS, Akkudoktor-EOS are Python services; evcc's MILP optimiser is not importable). Revisit after phases 3-5 using real data.
- Dishwasher and washer scheduling through Home Connect (1000 requests/day, 5 starts/min). See the Home Connect research note in PR #639.
- Battery support (none today).

## 10. Phases and exit criteria

| # | Phase | Exit criterion (empirical) |
|---|---|---|
| 0 | Decisions: #597 room model, #651 merged, hardware answers | #597 decided in writing; #651 merged |
| 1 | Linky TIC reader + Go parser + read-only surplus; fix the hysteresis weakness | Answers recorded: `SINSTI` on a no-sale install, frame period, VA-versus-W error; one week of surplus data in `events.db`; regression test that fails without the debounce fix |
| 2 | Provider abstraction; Beem refactor; Mater/Deye over Solarman V5 | Port 8899 test result recorded; Beem behaviour unchanged (existing tests); second `RolePV` provider reading live |
| 3 | Allocator + pool pump as first load, summer only | Live run: grid exchange stays near 0 W through a sunny day; fallback proven by stopping the daemon mid-run |
| 4 | Comfort loads via the room model | Heaters follow occupancy and setpoint; daemon-down fallback demonstrated on a real heater |
| 5 | Water heater | Nameplate power measured; chosen actuation option demonstrated; thermostat-satisfied detection verified |
| 6 | Deferrable appliances | Dishwasher starts inside a surplus window through Home Connect |

Each live-device phase starts by capturing the device's complete before-state (KVS, schedules) per the project's live-device discipline.

## 11. Open questions

1. Mater panels/logger: model, and is TCP 8899 open? (Panels not yet bought.)
2. Linky: `SINSTI` on a no-sale install; frame period.
3. Water heater nameplate power; where to meter.
4. Office heaters: type, power, switching, own thermostat.
5. Strict priority or proportional sharing.
6. ~~Pool pump versus water heater order~~: decided, pool pump first in summer, water heater first in winter (section 4.4).
7. Legionella / minimum-temperature policy for the water heater.
8. Allocator packaging: part of the `myhome` daemon (proposed) or a separate service.
