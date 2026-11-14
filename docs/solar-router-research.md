# Solar router / PV surplus diverter: prior art and design inputs

Status: research note, not a design and not an implementation plan.
Branch: `worktree-feature+solar-router`. Progress log: `docs/solar-router-research-progress.md`.
Looked at: 2026-10-31 (today). Versions and "last pushed" dates are as reported by the GitHub API on that day.

## What was asked

Survey public approaches and community implementations of a PV-surplus "solar router" / diverter / home energy management (HEMS) load-allocation system, and assess what could be integrated, ported or borrowed for a Go (preferred) or Shelly-JavaScript implementation in this repo. Consumers in scope: pool pump (already automated), 300 L resistive water heater, dishwasher (Home Connect), later a washing machine. Providers in scope: Beem (exists), "Matter France" (see open questions), generic meters and inverters.

## Summary

1. The repo already has the first half of a solar router: a daemon-side `SolarAggregator` that republishes a retained `myhome/energy/solar/available` value, and a device-side hysteresis in `pool-pump.js` that consumes it. There is one provider (Beem, production only) and one consumer (pool pump). There is no grid-meter signal, no allocator, and no second consumer.
2. The most relevant open-source reference is evcc (Go, MIT, very active). Its useful ideas are the site surplus formula (grid meter plus PV, battery and auxiliary meters, with a residual-power target), separate enable and disable delays, and a priority scheme that reserves surplus for higher-priority loads. Its code is a monolithic application, its charger abstraction models EV chargers, and some components need a sponsor token. I would borrow design, not import the library.
3. The Home Assistant diverter community (EMHASS solar diverter, Shelly-dimmer PID setups, Solar-Router-for-ESPHome) converges on a PID loop that drives grid export to a setpoint (usually 0 W) through a proportional actuator (dimmer, 0-10 V regulator, or SSR). A resistive water heater is the common load. These are small, but several are GPL or have no licence, so copying code is a licence question while reading them for ideas is not.
4. Provider reality: Beem has no official API. Its cloud endpoints are reverse-engineered and already used by the repo. The Shelly Pro 3EM documentation does not state a power sign convention, so import/export direction must be measured before any design depends on it. Forecast.Solar and Open-Meteo have usable free tiers with restrictions (Open-Meteo's free tier is non-commercial). Home Connect allows 1000 API requests per day per client and user, and 5 program starts per minute.
5. Recommended for a deeper design look: (A) add a grid-meter signal to the existing aggregator and fix the start-hysteresis weakness the repo already measured; (B) a small Go allocator that borrows evcc's surplus and priority concepts, with device-side safety kept on each Shelly; (C) a device-side proportional controller for the water heater. Defer (D) an EMHASS/EOS-style optimiser for the dishwasher and washer.

## Local baseline (what already exists)

All paths are relative to the worktree root. These are facts about the repo, verified by reading the files.

| Piece | Where | What it does today |
|---|---|---|
| Solar source interface | `myhome/daemon/solar_source.go` | `SolarSource` with `Name()` and `Subscribe(ctx) <-chan SolarReading`. `SolarReading` has `Source`, `Watts`, `TS` only. No "kind" (production vs grid), no sign convention. |
| Only source | `myhome/daemon/solar_beem_source.go` | Wraps `pkg/beem.Watcher.PowerCh`. Production watts only. |
| Aggregator | `myhome/daemon/solar_aggregate.go` | Sums last reading per source, drops sources older than `staleAfter`, publishes retained JSON `{available_w, ts (unix s), sources[]}` to `myhome/energy/solar/available` on every incoming reading. No periodic republish. Documents that two sources could publish out of order (no lock across publish). |
| Beem client | `pkg/beem/client.go`, `types.go` | Endpoints `api-x.beem.energy/beemapp/user/login`, `/beemapp/box/summary`, `/beemapp/devices`. `wattHour` is instantaneous watts despite the name. `weather` is always null in captures. Poll 60 s default. |
| Static claimers registry | `internal/myhome/energy/registry.go`, `myhome/daemon/solar_rpc.go` | Name plus device ID for claimers. Only `pool-pump` is registered. Explicitly "not a live-arbitration engine". A test uses a `water-heater` name as data only. No water-heater code exists. |
| Pool-pump hysteresis | `internal/shelly/scripts/pool-pump.js` (SOLAR block, `checkSolarHysteresis()`) | Subscribes to the retained topic, judges staleness from payload `ts` (not receipt time), start threshold 500 W with 5 min hold, stop threshold 200 W with 10 min hold, 30 s tick, calls its own `doStart`/`doStop`. Stale or absent means the schedule decides alone. Defaults in `docs/pool-pump.md` table. |
| Measured start-hysteresis weakness | `docs/integration-tests.md` (around lines 600-655, live test 2026-08-30) | Live feed alternated 360 and 420 W about every 45 s. The start-side code resets its 5-minute hold on any single sample below threshold, with no debounce or averaging. The shipped 500 W default was therefore unreachable; threshold lowered to 300 W for the test. The doc's own phrasing: "The threshold is a trough setting, not a \"how much sun do you want\" mean setting". |
| Heater script | `internal/shelly/scripts/heater.js` | A Kalman-filter house-heating controller (not a water heater). Uses Open-Meteo. |
| Config | `docs/configuration.md` (around lines 530-545) | `beem.email`, `beem.password`, `beem.poll_interval`; `solar.stale_after` default `5m`. |
| Matter RPC | `pkg/shelly/matter/` | Exists (Matter.* RPC for Shelly). Not examined further; relevant only if "Matter France" turns out to mean the Matter protocol. |
| Licence | `LICENSE` | Mozilla Public License 2.0 (MPL-2.0). Relevant to what can be copied from elsewhere. |

Local design observations (not recommendations):
- The current `SolarReading` cannot express a grid-import/export signal. Adding one changes the aggregator's contract, because `available_w` is currently a plain sum of producers.
- The pool pump's solar decision is a pure threshold with delays, applied per sample. That is the exact behaviour the 2026-08-30 test showed to be fragile under a noisy feed.
- The daemon-optional rule (`AGENTS.md`, "Rule 2") is already satisfied for the pump: stale topic means schedule-only. Any allocator must keep an equivalent device-local fallback.
- The internet-optional rule (`AGENTS.md`, "Rule 1") applies to Beem, Forecast.Solar and Open-Meteo. Each needs a timeout and a no-op or cached fallback.

## Comparison of prior art

Licence and activity are from the GitHub API on 2026-10-31 unless stated. "Architecture" is what the source or primary docs show.

| Project | Language | Licence | Activity (last push) | Role | Provider / consumer model | Relevance |
|---|---|---|---|---|---|---|
| evcc (evcc-io/evcc) | Go | MIT (sponsor-required components excluded) | 2026-10-31; 7,478 stars | Full EV charging and site energy manager | Site with grid/PV/battery/aux/consumer meters; loadpoints (charger + vehicle) with priority; optimiser (MILP) | **High**: best Go reference for surplus and priority |
| EMHASS (davidusb-geek/emhass) | Python | MIT | 2026-10-31; 719 stars | Linear-programming optimiser for Home Assistant | Deferrable loads, forecasts, HA integration | Medium: deferrable scheduling ideas |
| EMHASS solar diverter (davidusb-geek/emhass-solar-diverter) | Python (HA script) | MIT | 2026-03-09; 3 stars; created 2026-02-28 | PID diverter to a Shelly Dimmer | One dimmer, one water heater | Medium: concrete PID-to-dimmer example |
| Akkudoktor-EOS (Akkudoktor-EOS/EOS) | Python | Apache-2.0 per fetched LICENSE file; GitHub reports NOASSERTION | 2026-10-31; 1,722 stars | Energy optimisation service (battery, EV, heat pump, household) | Genetic optimiser over 15-min plans | Medium: reference for an optimiser service |
| OpenHEMS (abriotde/openhems-sample; openhems.readthedocs.io) | Python | GPL-3.0 (sample repo) | 2026-08-30; 10 stars | HA-based optimisation layer; uses EMHASS; RTE Tempo supported | Optimiser issues commands, HA executes | Low-medium: French tariff relevance |
| mk2PVrouter firmware (FredM67/PVRouter-1-phase) | C++ (Arduino) | LGPL-2.1 | 2026-10-26; 5 stars | Single-phase Mk2 router firmware (refactor of Robin Emley's sketch) | Resistive load, zero-crossing burst firing | Medium: classic algorithm lineage |
| xlyric/pv-router-esp32 (APPER, French) | C++ (ESP32) | README says CC BY-NC-SA 4.0; GitHub reports "Other" | 2026-08-03; 47 stars | French DIN-format PV router, ESP32 | Measures via SCT013 clamp or Shelly EM; dimmers, SSR or relays; per-load scheduler | **High for ideas, not for code** (non-commercial licence) |
| Solar-Router-for-ESPHome (hacf-fr, moved from XavierBerger) | ESPHome / YAML | GPL-3.0 | 2026-10-29; 36 stars | ESPHome router, triac/SSR/relay | "progressive" and "ON/OFF" routing | Medium: multi-output idea |
| Shelly Pro EM Home Assistant PID router (mathieucarbou gist) | HA YAML/package | Not stated | Gist (2024-era, screenshots dated 2024-09) | PID to Shelly Dimmer, LSA regulator | Single load | Medium: Shelly-dimmer PID tuning ideas |
| Node-RED PID diverter (SergioRius flow) | Node-RED / ESP8266 | Not verified | Not verified | PID in Node-RED, PWM setpoint over MQTT, watchdog | Single load | Low-medium: MQTT setpoint and watchdog pattern |
| SimpleDerivador (diegargon) | Python + ESP32 dimmer | None stated (GitHub: no licence) | 2024-05-26; 0 stars | Derivador for a Chinese dimmer board, reads Shelly EM over MQTT | Single load | Low (no licence) |
| OpenEVSE / ESP32 WiFi gateway (julianwb) | ESP32 | Not verified | Not verified | Eco mode with smoothing; surplus 1.4 kW threshold | EV | Low: smoothing idea only |
| PvPilot (Toulouse, French HEMS) | Commercial | n/a | Article 2026-02-16 | Commercial HEMS, water heaters, batteries, EV | Relay contactors in panel | Reference for French market only; no public API found |
| myPV ELWA 2 / AC ELWA | Commercial hardware | n/a | Cloud API in BETA (my-pv, 2024-10-07) | Stepless 3.5 kW water heater with its own surplus logic | Feeds from a meter at grid point | Hardware option; local protocol not public |
| Fronius Ohmpilot | Commercial | n/a | n/a | Surplus water heater controlled by the inverter | Inverter sends setpoint, Modbus RTU over RS485 | Architecture reference (inverter-as-controller) |
| SolarAssistant | Commercial/OSS (licence not established) | Not established | n/a | Inverter data logger with MQTT API | Data source, not a router | Provider-side reference |
| OpenEnergyMonitor PV diversion docs | Docs | n/a | Docs | Diverter theory (burst, phase control, PWM) | Theory | High for control theory |

### Per-project notes

**evcc.** Single Go module `github.com/evcc-io/evcc` (`go.mod`). Notable dependencies: `grid-x/modbus` (replaced by `evcc-io/modbus`) and `andig/gosunspec`. Sources: [github.com/evcc-io/evcc](https://github.com/evcc-io/evcc), [go.mod](https://github.com/evcc-io/evcc/blob/master/go.mod). Full analysis in the evcc section below.

**EMHASS.** README: "The package uses a Linear Programming approach to optimize energy usage." It describes HA integration and "Retrieve sensor data directly from Home Assistant and publish optimized schedules back" on the docs site. Its docs say "Manage thermal inertia for water heaters and heat pumps effectively to act as thermal energy storage," linking a "Deferrable load thermal model" page that I could not fetch (404). Sources: [README](https://raw.githubusercontent.com/davidusb-geek/emhass/master/README.md), [docs](https://emhass.readthedocs.io/en/latest/). Licence text: MIT, "Copyright (c) 2021-2026 David HERNANDEZ TORRES".

**EMHASS solar diverter.** Python HA script, PID to keep grid export at 0 W, script called every 5 s per the page, drives a Shelly Dimmer 0/1-10 V PM Gen3 which drives an LCTC regulator. The page says: "You must ensure the 0-10V output is **Current Sourcing**, NOT Current Sinking." It has voltage-brownout shutdown and optional temperature probes. It states no PID gains, sample time or output limits beyond the 5 s call. The author warns: "You must mount this on a proper aluminum heatsink." Source: [repo](https://github.com/davidusb-geek/emhass-solar-diverter), [README](https://raw.githubusercontent.com/davidusb-geek/emhass-solar-diverter/main/README.md).

**Akkudoktor-EOS.** README describes a "GENETIC" optimiser with 15-minute plans, battery, EV with departure targets, PV and "flexible household loads". Heat pumps and water heaters are not named in the fetched README text. HTTP API with OpenAPI. Licence: the fetched `LICENSE` file is Apache-2.0 text, copyright "Dr. Andreas Schmitz". GitHub's detector returns NOASSERTION. This conflict should be resolved by reading the repository directly before reuse. Sources: [repo](https://github.com/Akkudoktor-EOS/EOS), [LICENSE](https://raw.githubusercontent.com/Akkudoktor-EOS/EOS/main/LICENSE).

**OpenHEMS.** Self-described as "an Energy Management System (HEMS) that sits on top of your home automation system" and "not another home automation platform". Home Assistant does device I/O. Supports RTE Tempo ("Support multiple off-peak time-slots and even variable time-slots and cost (RTE Tempo contract)"). Maturity statement: "under activ developpment and is used on production but remain at early developpment." The docs page gives no licence; the sample repo is GPL-3.0. Sources: [docs](https://openhems.readthedocs.io/en/latest/intro.html), [sample repo](https://github.com/abriotde/openhems-sample).

**Mk2 lineage and OEM diverter theory.** The FredM67 README describes the code as a refactor of Robin Emley's "Mk2_fasterControl_twoLoads_x" sketch, with time-critical work moved into the ISR and "Delayed diversion start". Its README does not describe the control algorithm or the measurement method. The OpenEnergyMonitor forum summaries say Mk2 uses burst firing of whole mains cycles with zero-crossing switching. Those are secondary; the OEM diverter page is primary for the theory. From that page (quoted):
- Burst mode: "Switching happens at zero-crossing." Burst mode is "the mode generally preferred by amateur constructors."
- Phase control: "the resulting voltage transient can generate harmonics that can cause undesirable heating in motors"; "Heavy and expensive filters are required to keep the harmonics within acceptable limits."
- PWM: "we aren't aware of any constructor built systems."
- Burst mode flicker: "Flicker is worst with a weak supply and a large load."
Sources: [FredM67 repo](https://github.com/FredM67/PVRouter-1-phase), [OEM diverter choice](https://docs.openenergymonitor.org/pv-diversion/introduction/choosing-an-energy-diverter.html).

**xlyric/pv-router-esp32 (APPER).** README (fetched): open-source PV router on a TTGO T-Display; integrates with MQTT, Home Assistant, Jeedom and Domoticz; supports Shelly EM and Enphase Envoy as external sources; Dallas DS18B20 temperature; "per-load time scheduler"; "2-hour boost mode"; fuse and temperature safety. Its stated aim is to keep grid exchange near 0 W. Outputs: remote AC dimmers (PV-Dimmer), local triac dimmer up to 20 A or SSR, two relays. Licence per README: CC BY-NC-SA 4.0. GitHub reports "Other". Non-commercial plus ShareAlike: copying code into this repo is a licence question at minimum, so I treat it as idea-only. Sources: [repo](https://github.com/xlyric/pv-router-esp32), [README](https://raw.githubusercontent.com/xlyric/pv-router-esp32/main/README.md).

**Solar-Router-for-ESPHome (hacf-fr).** Docs say routing is "a choice of dynamic energy routing algorithms (progressive, ON/OFF)". Regulators: Triac, SSR, mechanical relay. Safety: temperature limiter and a scheduler (forced run). It does not describe PID or a watchdog in the fetched overview. Sources: [repo](https://github.com/hacf-fr/Solar-Router-for-ESPHome), [docs](https://hacf-fr.github.io/Solar-Router-for-ESPHome/). The XavierBerger repo redirects to hacf-fr.

**Shelly-dimmer PID setups (mathieucarbou gist and similar).** The gist describes a PID controller driving a Shelly Dimmer into an LSA voltage regulator; it recommends calibrating the dimmer's 0 % and 100 % points with the Shelly remap function, and suggests output 9-75 for LSA/LCTC regulators. It starts with Kp = 1 and gives no Ki or Kd. Source: [gist](https://gist.github.com/mathieucarbou/f9215b311a9018623c1579ab5c2cad7a). Licence not stated.

**Shelly official HA project (search snippet only).** Shelly's blog describes a Shelly 1PM switching a heater, two Shelly 1 relays for a voltage regulator, and a 3EM for metering, triggered when the sum of three phases goes negative. Only the search snippet was read; I did not fetch the page. Source: [Shelly blog](https://us.shelly.com/blogs/customer-projects/using-photovoltaic-energy-for-water-heating).

**Fronius Ohmpilot (search snippet only).** The inverter drives the heater's setpoint from the Smart Meter measurement. Modbus RTU over RS485 is recommended. Fault mode: if the data link breaks, the element tries to reach 60 °C "every several hours", which can draw grid power. Source: [Fronius support](https://www.fronius.com/en/help-center/solar-energy/solar-powered-water-heating/support-ohmpilot) (search snippet; fetch not attempted).

**myPV ELWA 2.** Surplus information comes from a feed-in meter at the grid point; the control type must be set to HTTP to accept external control. The official my-PV API is a cloud REST API, in BETA as of 2024-10-07, with an API token and endpoints that "may still be changes". The local JSON or control interface is not public: one HA user reports the documentation must be requested from my-PV. The evcc docs page for `ac-elwa-2` lists only a host and temperature-source parameter. Sources: [my-PV API news](https://www.my-pv.com/en/news/flexible-control-with-the-my-pv-api), [evcc ac-elwa-2 docs](https://docs.evcc.io/en/meters/my-pv-ac-elwa-2). Community HA threads are not cited as evidence.

## Control algorithms

### Surplus calculation

Two families appear across the sources:

1. **Grid-point measurement (export-referenced).** Measure power at the grid connection; the target is a grid exchange near a setpoint. The HA diverters and the Mk2/APPER routers do this. Consumed power of the controlled load is already inside the grid reading, so a single meter works. The downside is that it fails if the meter is not at the grid point.
2. **Production minus consumption (site balance).** evcc does this in `sitePower()`. The fetched summary of `core/site.go` gives:
   - `residualPower` = the household margin, read from the site configuration.
   - If there is no grid meter, `gridPower` is estimated as `totalChargePower - pvPower`.
   - The core balance is `sitePower := gridPower + batteryPower + excessDCPower + residualPower - auxPower - flexiblePower`, with `pvPower` only estimated when a meter is missing.
   - Doc comment on `sitePowerResult.power`: "net power exported by the site minus a residual margin (negative values mean grid: export, battery: charging)".
   - Source: [core/site.go](https://github.com/evcc-io/evcc/blob/master/core/site.go). This was read via a summary of the file; I did not check the exact line numbers.
3. **Evcc site docs (primary, quoted):** "To regulate charging with solar surplus, a readable meter directly behind the grid connection point is necessary." Auxiliary meters for self-regulating devices "such as immersion heaters" count toward surplus; consumer meters are for statistics only. `residualPower` defaults to 0 W; positive values aim for export. Source: [evcc site reference](https://docs.evcc.io/en/reference/configuration/site).

The repo's current production-only signal (Beem) cannot compute either family. The Beem docs say so explicitly: "With a PnP kit only, the REST API returns solar production figures but no home consumption and no grid draw."

### Enable and disable control (evcc)

From `core/loadpoint.go` (`pvMaxCurrent`, fetched summary):
- Enable and disable are separate timers (`pvTimer` shared) with `Enable.Delay` default `time.Minute` and `Disable.Delay` default `3 * time.Minute`.
- Thresholds: `Enable.Threshold` and `Disable.Threshold` default 0. If set, they override derived thresholds. The derived disable threshold is `(1 - effectiveSolarShare) * CurrentToPower(minCurrent, phases)`.
- `solarShare` defaults to 1.
- `minCurrent` default 6 A (also the PV start current), `maxCurrent` default 16 A.
- The summary states the term "hysteresis" does not appear. The separated thresholds and delays serve that purpose.
- The guard duration in YAML (`guardduration`) is deprecated and ignored.
- Not established from the summary: whether one sample back below the threshold resets an enable timer. The summary does not say, so do not assume either way.

Source: [core/loadpoint.go](https://github.com/evcc-io/evcc/blob/master/core/loadpoint.go).

The repo's pool pump uses 5 min start and 10 min stop delays (`docs/pool-pump.md`). evcc uses 1 min enable and 3 min disable for EVs. Both are per-consumer choices, not universal values.

### Priority and fair share (evcc)

`core/prioritizer/prioritizer.go` (fetched summary):
- `Prioritizer` holds `demand map[loadpoint.API]float64`, updated from each loadpoint's `GetChargePowerFlexibility(rates)`. Negative results are ignored, so the previous entry stays.
- `GetChargePowerFlexibility(lp)` sums demand of entries with strictly lower `EffectivePriority()` and positive power. It returns that sum as the extra headroom the caller can use.
- It does not distribute surplus, has no hysteresis, no timers, no run loop. Entries are never removed, and a loadpoint that stops charging keeps its last positive value until it reports zero.
- The summary notes a possible data race: `GetChargePowerFlexibility` reads `demand` without taking the mutex that `UpdateChargePowerFlexibility` takes. Not verified by running the race detector.
- Priority semantics from the loadpoint docs (quoted in the search summary, not fetched in full): "In minpv mode, charging is not interrupted, only reduced to the minimum." Higher-priority loads "can also start while a lower-priority one is charging".
- `core/site_prioritize_test.go` asserts that a lower-priority loadpoint reserves surplus equal to the higher one's max power while that one is starting, and that the reservation drops to zero once it is charging. It does not test how power is split among loads already charging.

Source: [core/prioritizer/prioritizer.go](https://github.com/evcc-io/evcc/blob/master/core/prioritizer/prioritizer.go), [site_prioritize_test.go](https://github.com/evcc-io/evcc/blob/master/core/site_prioritize_test.go).

Practical observation: evcc gives a strict priority order with reservation, not a proportional fair share. For a pump, a heater and a dishwasher that is the likely shape of the answer, but this is an inference from the code reading, not a documented policy.

### Proportional, PI and PID control

- Burst fire (Mk2, triac): switches whole mains cycles at zero crossing. Burst mode is "the mode generally preferred by amateur constructors"; flicker is its main problem. OEM diversion page.
- Phase control: one switch per mains cycle; harmonics; filters are expensive. OEM diversion page.
- PWM with MOSFET or IGBT: fine control, rarely built. OEM diversion page.
- External proportional regulators (0-10 V or Shelly Dimmer into an LSA/LCTC regulator): the HA projects use PID on grid export. EMHASS solar diverter: "Rapidly adjusts the dimmer to keep grid export at exactly 0W." Its gains are not published in the fetched README. The mathieucarbou gist starts with Kp = 1.
- Solar-Router-for-ESPHome and xlyric: the fetched texts do not name a PID; xlyric says it aims to keep grid exchange near 0 W, and does not state the algorithm.

Negative result: I did not establish a documented gain set or a tuning method that transfers to a Shelly Dimmer. The sources give the approach, not the numbers.

### Anti-flapping and noise (repo evidence is the strongest)

- Repo: start-side hysteresis resets on any single sample below threshold, with no averaging; measured on 2026-08-30 with a 45 s oscillation between 360 and 420 W (`docs/integration-tests.md`). The fix applied in the test was to lower the threshold, not to add averaging. This is a documented weakness, not an endorsed design.
- evcc: separate enable and disable thresholds with delays are its anti-flap mechanism, per the summary. Not verified whether it averages.
- Burst firing and dimmer approaches introduce their own flicker. OEM page.

### Safety and overrides

- Repo pool pump: water supply protection and the anti-cycling fuse come before solar; a solar-driven start goes through the same `desiredOutput()` and cannot bypass them (`docs/pool-pump.md`, around lines 108-145).
- HACF Solar-Router: temperature limiter; scheduler for a forced run.
- EMHASS solar diverter: brownout shutdown on grid voltage; optional tank temperature probes. The author states the tank probes "will void your warranty" when drilled in.
- xlyric: fuse and temperature safety; 2-hour boost mode.
- Legionella and minimum temperature for a water heater: not covered by any of the fetched sources, so I cannot cite a requirement. This is a gap in the research.

### Minimum on/off times

- Repo: pool pump hard-coded minimum via filtration logic and delays; no general minimum run time was found.
- evcc: `minCurrent` and phase switch guards (`phaseSwitchDuration` 60 s, `chargerSwitchDuration` 60 s).
- OEM burst mode: "minimum on/off times of a few milliseconds" for zero-crossing switching, which is an electrical rather than a thermal limit.

### Phase handling

- evcc: phase switching `pvScalePhases` with `phaseScale1p` and `phaseScale3p` timers; `minActiveCurrent` 1.0 A and `minActiveVoltage` 207 V to count active phases.
- Shelly Pro 3EM: a three-phase energy meter with per-phase `a_act_power`, `b_act_power`, `c_act_power` and `total_act_power`. Source: [EM component docs](https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/EM).
- Fronius: a three-phase heater needs a neutral and a star connection. Single-phase elements 0.3-3 kW, three-phase 0.9-9 kW. Source: Fronius support snippet.

### Deferrable loads (dishwasher, washer)

- Home Connect: `BSH.Common.Option.StartInRelative` appears as the "Start in Relative Option" label on the dishwasher section of the Home Connect API docs (fetched index page). The docs site returned only labels for this option, not its semantics. The HA docs describe it as a delay in seconds from when the command is sent. Sources: [Home Connect docs](https://api-docs.home-connect.com/), [HA action docs](https://www.home-assistant.io/actions/home_connect.set_program_and_options/) (search snippet, not fetched).
- Home Connect remote-start prerequisites (remote control enabled, remote start allowed, Ready state) are reported by a third-party plugin's description, not verified against the official docs. Treat them as unverified.
- EMHASS and EOS optimise over a horizon with forecasts. EOS says it supports "flexible household loads", but its README does not describe a deferrable-start mode for appliances. EMHASS README: "an optimized daily schedule for your devices." Neither README gives the device model in enough detail to decide on a dishwasher.

### Forecast-aware planning versus reactive control

- Reactive: all sources reviewed above. The pool pump is reactive plus a schedule.
- Forecast-aware: EMHASS and EOS (LP and genetic); evcc optimiser (MILP, 15-minute slots, horizon equal to available forecast). The evcc optimiser page says: "uses mixed integer linear programming (MILP) to minize a cost function" and that multiple-loadpoint coordination is not explained there.
- Forecast sources: Forecast.Solar free tier and Open-Meteo (see Providers).

## Architecture patterns

### Provider and consumer abstraction

- evcc: a `Site` owns meters grouped by role (`grid`, `pv`, `battery`, `aux`, `consumer`, `ext`). Loadpoints own a `Charger` plus vehicle. The `api/api.go` interfaces include `Meter`, `Charger` (`Enable(bool) error`, `MaxCurrent`), `Dimmer` (`Dim(bool)`), `Curtailer`, `CurrentLimiter`, `PhaseSwitcher`, and `CircuitLoad` (sources: [api/api.go](https://github.com/evcc-io/evcc/blob/master/api/api.go)). Meter roles are declared in config, not inferred.
- EMHASS and EOS: optimiser service that consumes HA entities and publishes schedules; devices are driven from HA.
- OpenHEMS: optimisation layer over HA; HA executes commands.
- Repo: a `SolarSource` interface for producers and a static `energy.Registry` for consumers. The two are not linked, and the registry does no arbitration.

Observation: the repo's `SolarReading` has no role field. evcc's role-based meter model is the more explicit alternative.

### Data model and cadence

- Shelly Pro 3EM: energy is stored at one-minute intervals ("stores it on one minute intervals", Pro 3EM device page); `active_power_change` fires at "at least 10W and 5%" change (EM component page). The Pro 3EM documentation does not state a sampling rate and does not state a sign convention (the Pro 3EM device page gives no sign convention; the fetched summary said so explicitly).
- Beem: 60 s poll (`pkg/beem`).
- Forecast.Solar: forecasts refresh "at the earliest every 15 min".
- Home Connect: 1000 requests per day, 50 per minute, opening an SSE event stream counts as one request.
- Open-Meteo: free tier under 10,000 calls per day, 5,000 per hour, 600 per minute.

### Where decisions run, and degraded mode

- Repo today: decision on the device (pool pump), daemon only publishes an aggregate. Daemon down: device falls back to schedule (`docs/beem-energy.md`, pool-pump section; `docs/pool-pump.md`).
- evcc: a single central process decides for all loadpoints. Degraded mode is not described in the fetched sources.
- Fronius Ohmpilot: decision on the inverter. Data link broken: element tries to reach 60 °C every several hours, drawing grid power. Lesson: a device must have a known, safe default when its controller is absent.
- HACF Solar-Router, EMHASS diverter, xlyric: local controller or HA script; degraded mode depends on HA being up.

### Failure modes observed in the sources

- Stale retained value: the repo already handles this using payload `ts`, not receipt time (`pool-pump.js` comment and `solar_aggregate.go`).
- Provider outage: Beem depends on the cloud; the aggregator simply stops updating.
- Meter outage: evcc requires a grid meter for solar charging; a missing meter estimates from other meters (per `sitePower`).
- Communication loss to a dimmer or regulator: Fronius data-link failure case above.
- Race between publishes: documented in `solar_aggregate.go`; harmless with one source.

## evcc: suitability for this project

Question asked: could meter, charger or loadpoint abstractions, site or prioritiser logic, tariffs or forecast, or templates be imported as a library, ported, or used as design reference?

| Element | Import as library? | Port? | Reference only? | Notes |
|---|---|---|---|---|
| Site surplus formula (`core/site.go`) | Not practical | Yes, small | Yes | Formula is simple; grounded in code read. |
| Loadpoint PV-mode timers and thresholds (`core/loadpoint.go`) | No: tied to loadpoint and charger types | Yes, concept | Yes | Concept of separate enable and disable delays. |
| Prioritiser (`core/prioritizer`) | No: depends on loadpoint API | Concept only | Yes | Demand reservation idea; has a possible data race (unverified). |
| Charger interfaces (`api/api.go`) | No | No | Yes | Model is EV-shaped (current limits, phases, vehicle SoC). A pool pump or on/off heater is a poor fit. |
| Optimiser (MILP, `core/optimizer.md`) | No | Heavy | Yes | Needs a solver; overkill for three loads. |
| Modbus and SunSpec libraries (`grid-x/modbus`, `andig/gosunspec`) | Possible directly | n/a | n/a | These are separate modules, not evcc internals. See Go libraries below. |
| Templates (YAML device definitions) | Not applicable | Idea | Yes | Device templates keep vendor specifics out of code. |

Licence: MIT for the project as a whole. The README states "All sponsor-required components are excluded from the MIT License", and "one evcc sponsorship token is required per evcc instance" for them. Which components are gated is not listed in the README. The sponsorship page ties tokens to "many commercial EV chargers" and says the same applies to "simple switch-sockets and plugins" (wording as fetched). Meters, heaters, heat pumps and remote access are not explicitly mentioned there. I did not establish which evcc components a Shelly-based setup would need, so the licence status of any specific evcc path is unknown.

Charger fit:
- Pool pump (variable speed): evcc has no pool concept; a pump switch can be wrapped as an on/off `Charger`, but speed and turnover targets have no equivalent. Inference.
- Water heater (resistive): evcc's `aux` meter role covers "self-regulating devices such as immersion heaters" as surplus contributors. The docs describe them as self-regulating, not as loads evcc drives. Whether evcc drives a heater through its `Dimmer` interface is not established.
- Dishwasher: not a continuous load. evcc's planner is for vehicle charging targets. Inference: not a fit.

Negative result: I rejected importing evcc as a library. Reasons: a single application module (`github.com/evcc-io/evcc`) with a large dependency graph, types shaped for EV charging, and sponsor-gated components whose boundary is not published. I did not measure the dependency graph, so "large" is an impression, not a number. Ideas are reusable; code should be written fresh.

Licence compatibility for ideas: concepts (formulae, control structure) are not copyrightable in the same way as code, but I am not giving a legal opinion. Copying code: evcc (MIT) is compatible with MPL-2.0 for inclusion, subject to the sponsor-gate boundary. APPER (CC BY-NC-SA) and GPL/LGPL sources are not compatible with copying into this MPL-2.0 repo without a licence review. This is my reading, not legal advice.

## Go libraries worth considering

| Library | Licence | Last push | Use | Note |
|---|---|---|---|---|
| `simonvetter/modbus` | MIT | 2026-07-28; 456 stars | Modbus TCP/RTU client and server | Actively maintained (per API). Candidate for an inverter or Elwa-style register read. |
| `goburrow/modbus` | BSD-3-Clause | 2024-06-23; 1,042 stars | Modbus client | Not archived; less recent. |
| `andig/gosunspec` | BSD-3-Clause | 2026-07-05; 15 stars | SunSpec model types and Modbus scan | Used by evcc (`go.mod`). Good fit for SunSpec inverters and meters. |
| evcc `grid-x/modbus` | per upstream | n/a | Modbus | Replaced in evcc by `evcc-io/modbus`. |

No Go library for PID or control loops was evaluated in depth; I did not search for one. Writing a small PI loop in Go is standard practice, so I did not survey alternatives. This is a gap, not a finding.

## Providers

### Beem Energy (existing)

- Official API: none. Evidence: CharlesP44's README says the integration is "Intégration non officielle" and not "officiellement affiliée, maintenue ou sponsorisée par Beem Energy" ([README](https://raw.githubusercontent.com/CharlesP44/Beem_Energy/main/README.md)). A search found no developer documentation. `developer.beem.energy` does not resolve (DNS ENOTFOUND on 2026-10-31).
- Cloud endpoints (reverse-engineered, used by this repo): `api-x.beem.energy/beemapp/...`, bearer JWT. Repo spec: `docs/beem-energy.md` and `pkg/beem/client.go`.
- Local API: none found. A 2023 HA thread states there is no local API (community claim, not verified). The French HA thread reports a planned port scan without a result. Unknown.
- Batteries: CharlesP44 lists "Beem Battery" as "Entièrement supportée" and reports real-time solar, battery and grid watts over MQTT. Its README does not mention a PnP device. The repo runs a PnP kit only, so it has no grid channel. The MQTT broker is not named in the README; whether it is Beem's cloud broker is unknown.
- Polling: CharlesP44 polls REST at 60 s for non-battery devices (per search snippet; README did not state the REST interval). Beem's own rate limits: not published, not established.
- Auth: login and token refresh as implemented in `pkg/beem`. Beem's terms of service: not read.
- Community clients: CharlesP44 (MIT, pushed 2026-10-24, Python, HA), ClaraVnk (MIT, pushed 2025-05-21, HA YAML). No Go client found.
- Implication: a grid signal from Beem requires a Battery; with the current PnP kit, the only provider signal is production. Matches `docs/beem-energy.md`.

Sources: [CharlesP44 repo](https://github.com/CharlesP44/Beem_Energy), [ClaraVnk repo](https://github.com/ClaraVnk/home-assistant-beem-energy).

### "Matter France"

I could not identify this provider. Searches returned:
- The Matter smart-home standard, not a provider. Matter 1.4 (CSA, released 2024-11-07) adds Device Energy Management, Water Heater Mode, and Solar Power device types. Its wording: "allows energy-consuming devices to adjust start times based on energy usage forecasts and power management needs" and "Electric water heaters can be set to a preset temperature or percentage". Source: [CSA Matter 1.4](https://csa-iot.org/newsroom/matter-1-4-enables-more-capable-smart-homes/). Shelly support for these clusters: not checked.
- A distributor called MaterFrance (solar equipment) in a French search result.
- PvPilot (Toulouse HEMS, 2026-02-16): controls water heaters, batteries and EV chargers; public API or local integration not stated. Source: [pv-magazine](https://www.pv-magazine.com/2026/02/16/french-startup-launches-universal-home-energy-management-system/).

So: no evidence of a French energy provider with a documented API under the name "Matter". The user needs to clarify (see open questions). I have not invented endpoints, polling rates or auth for it.

### Generic sources

- Shelly EM / Pro 3EM: the repo's own Shelly RPC and events are the cheapest grid source. EM component: per-phase `x_act_power` and `total_act_power`; events threshold-based (`active_power_change` at 10 W and 5 %). Pro 3EM device page: `em:0` (combined) and `emdata:0` (one-minute storage). No sign convention. The Pro 3EM page says storage is at one-minute intervals. Sources: [EM](https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/EM), [Pro 3EM](https://shelly-api-docs.shelly.cloud/gen2/Devices/Gen2/ShellyPro3EM/).
- SunSpec over Modbus: `andig/gosunspec` (see Go libraries). Inverter-side register maps are vendor-specific; Fronius and Deye examples exist but I did not fetch register maps.
- MQTT inverter sources: SolarAssistant publishes monitoring over MQTT and "inverter settings changes can also be performed" (its MQTT help page, per search snippet). Licence not established.
- Enedis Data Connect (Linky): community sources report a consumption load curve with 30-minute average power, requiring user consent and an application contract. The official portal was not reached. One community example repo appears to publish a client ID, secret and token in plain text (per the search summary, not checked by me); none of them should be reused. Sources: [consometers/data-connect](https://github.com/consometers/data-connect) (community). Not established from official docs.

### Forecasts

- Forecast.Solar (official): public tier "free for life" with "no API key", "12 calls per IP" per rolling 60 min, data horizon "+1 day", resolution "1 h". Next tier: 60 calls per API key, "+3 days", "30 min". Forecasts "updated at the earliest every 15 min". Sources: [account models](https://doc.forecast.solar/_export/xhtml/account_models), [API](https://doc.forecast.solar/api). Attribution and commercial terms: not established (the API page has only the wiki licence, CC BY-SA 4.0).
- Open-Meteo (official terms): "You may only use the free API services for non-commercial purposes." Free tier "less than 10'000 API calls per day, 5'000 per hour and 600 per minute". Data under CC-BY 4.0. Source: [Open-Meteo terms](https://open-meteo.com/en/terms). Repo already uses Open-Meteo for `heater.js` and the pool pump forecast, so the non-commercial position matches a hobby project (inference, not a legal reading).

### Tariffs (France)

- RTE Tempo: the RTE data portal API "Tempo Like Supply Contract" needs an account and token (secondary source: linuxfr article). The data.gouv.fr dataset describes the Tempo signal: "40 white days and 20 red days, and all remaining days are blue" and is published by RTE. Its licence field reads "License Not Specified", and the footer default is Open Licence 2.0. Update frequency not respected. Last update 14 September 2026 (as shown). Sources: [data.gouv Tempo dataset](https://www.data.gouv.fr/en/datasets/tempo-like-supply-contract/), [linuxfr article](https://linuxfr.org/news/tempo-saison-2026-2027-couleurs-et-previsions-en-acces-libre-sans-jeton). Token-free third-party endpoints exist (api-couleur-tempo.fr, calendrier-tempo.fr, including forecasts with CC BY 4.0 per the operator). Third-party: not official, not verified.
- Surplus injection price (EDF OA and resale): the sources I found disagree on the number and on whether a prime exists. I did not establish the current rate. Do not use any figure from this note. Check the Journal officiel arrêté and EDF OA's current grid.
- HP/HC: `pkg/shelly/schedule/jobs.json` has a label "Heures Creuses EDF" for a schedule. No tariff API established.

### Home Connect (dishwasher, washer)

- Quotas (official index page): "1000 requests per client and Home Connect user account per day", "50 requests per client and Home Connect user account per minute", "5 program starts per client and Home Connect user account per minute", "10 event monitoring channels per client", leaky bucket 10 requests/s with burst 20.
- Events: "The channel ... will stay open and you receive all changes." SSE: "Opening an SSE stream counts as 1 request toward the daily limit." Source: [Home Connect API docs](https://api-docs.home-connect.com/).
- Auth: OAuth2 authorization-code and device flow are listed as sections; the auth page was not fetched, so the details are not established.
- Remote start prerequisites: from a third-party description only; official text not retrieved. Unverified.
- Counters are per (client_id, user). Community reports of the 1000-call daily block (search snippet, anecdotal).

### myPV, Fronius, and other hardware providers

See per-project notes. Local protocol details for myPV ELWA 2 are not public; the cloud API is in BETA. Fronius Ohmpilot is a closed system controlled by the inverter; third-party control was not established.

## Recommendation sketch

These are approaches worth a deeper design look, with trade-offs. None is an implementation plan.

### A. Extend the existing aggregator with a grid signal, and harden the start hysteresis (lowest risk, reuse)

- What: keep the daemon aggregate and device-side consumers. Add a grid-point power source (Shelly Pro 3EM or other grid meter) as a second kind of reading, so the consumer can see net export. Address the measured start-hysteresis weakness with averaging or a debounce on the existing pool-pump logic.
- Reuse: `SolarSource`, `SolarAggregator`, `SolarAvailablePayload`, `pool-pump.js` hysteresis.
- Trade-offs: needs grid-meter hardware and a verified sign convention (not documented). Changing the payload semantics affects the pool pump. Daemon-optional stays intact if the device keeps its own fallback.
- Measurement needed before design: sign convention and sampling rate of the chosen meter; the live Beem production noise band (already observed at 360-420 W in one sample series).

### B. Small Go allocator that borrows evcc's surplus and priority concepts (medium effort, port)

- What: replace the static `energy.Registry` with a registry of consumers having a role, a priority and a power estimate, and a function that grants surplus in priority order with reservation (evcc `prioritizer` idea). Publish per-consumer retained grants. Each Shelly keeps its own safety and local fallback.
- Reuse: concepts from evcc `core/site.go` and `core/loadpoint.go`; Go style from `internal/myhome/energy`.
- Trade-offs: a central decision point, so degraded mode must be designed (device-side defaults, documented as "degraded when daemon is down", per `AGENTS.md`). Must not import evcc.
- Open: whether allocation should be per-consumer proportional (fair share) or strict priority. The sources show strict priority with reservation; proportional sharing is not shown in any fetched source.

### C. Water heater as a device-side proportional controller (hardware-dependent)

- What: a Shelly-based controller for the heater, driven by a grid-export target, with a proportional actuator (Shelly Dimmer 0-10 V plus SSR or regulator, or a commercial heater with its own surplus logic). The daemon publishes a setpoint; the device keeps a safe default (for example, off, or a fixed low setpoint) when the setpoint is absent. Legionella and minimum temperature are not covered by any source I read; they need a separate requirement.
- Reuse: PID shape from the HA diverters (ideas only; their licences are GPL, MIT-with-no-licence, or unknown). Calibration step from the Shelly dimmer docs (remapping 0 %/100 %).
- Trade-offs: hardware choice drives everything. A commercial heater with its own surplus logic (myPV, Fronius) avoids PID work but depends on a closed or beta interface.

### D. Forecast-aware optimiser for deferrable loads (defer)

- What: a planner for dishwasher and washer start windows using a PV forecast (Forecast.Solar or Open-Meteo) and tariff signal. Options: port a simple window heuristic in Go, or call an external optimiser (EMHASS or EOS, both Python).
- Trade-offs: Home Connect quota (1000 requests/day) and remote-start conditions need verifying before any design. Forecast quality for the free tiers is not established. EMHASS (MIT) and EOS (Apache-2.0 per file) are the usable licences; the EOS licence metadata needs a second look.

## Open questions for the user

1. "Matter France": which provider is meant? If it is a French energy company, which product, and is there a documented API? If it means the Matter protocol, which devices (Shelly support for Matter 1.4 energy clusters is unchecked)?
2. Grid meter: which device (Shelly Pro 3EM, other), and where is it installed relative to the grid connection point? Accepting a sign-convention test before design?
3. Water heater: resistive element with SSR or dimmer, contactor on/off only, or a commercial device (myPV, Fronius)? Minimum temperature and legionella policy?
4. Allocation policy: strict priority (pump before heater before dishwasher) or proportional sharing? Which loads may be interrupted?
5. Licence stance: may the project copy code from GPL, LGPL, or CC BY-NC-SA sources, or only read them for ideas? (The repo is MPL-2.0.)
6. Commercial use: is any part of the stack used beyond personal use? This decides Open-Meteo and similar free tiers.
7. Forecast source: is a one-day forecast (Forecast.Solar free) sufficient, or do you want a three-day forecast (Forecast.Solar paid, or Open-Meteo under the non-commercial terms)?
8. Injection value: does the export price matter for the design, or is the goal purely self-consumption? The French tariff sources conflict, so I could not establish the current value.
9. Dishwasher: is the Home Connect account owned and registered as an API client? Remote start must be enabled on the appliance.

## Things evaluated and rejected

- Importing evcc as a Go library: rejected for coupling and licence reasons (see evcc section). Reference only.
- EV-oriented charger model for the pool pump or water heater: rejected as a poor fit, inference from `api/api.go`.
- APPER/xlyric code: rejected for reuse (CC BY-NC-SA); reference only.
- SimpleDerivador: no licence stated; reference only, not reused.
- myPV cloud API as the water heater control path: not rejected on technical grounds, but rejected for now because it is in BETA and its local interface is not public.
- Beem "grid" signal from the current PnP kit: not possible; Beem's own docs and CharlesP44's README support the production-only conclusion.
- Using a tariff or prime figure from a comparator site: rejected because the sources conflict.

## What I could not establish

- "Matter France": identity, API, auth, polling, rate limits. Not found.
- Beem local API, Beem rate limits and terms of service, and whether the Beem MQTT broker is cloud-hosted.
- Shelly Pro 3EM power sign convention and sampling rate. The official device page does not state them.
- evcc: whether a single low sample resets its enable timer; whether it averages; which evcc components are sponsor-gated.
- evcc: whether its `Dimmer` interface is used to drive a heater.
- evcc: whether the `prioritizer` data race is real. Not run under the race detector.
- Akkudoktor-EOS licence: LICENSE text is Apache-2.0 but GitHub reports NOASSERTION. Not resolved.
- SolarAssistant licence. Not found.
- Mk2 Robin Emley algorithm details: the primary pages (mk2pvrouter.co.uk and the "About" text files) returned 404 at the tested URLs. The burst description comes from forum summaries and the OEM diverter page.
- Home Connect remote-start prerequisites and OAuth details: the official auth and program pages were not fully read. The "24 h rule" mentioned by third parties is unexplained.
- Forecast.Solar attribution and commercial terms beyond the wiki licence.
- Enedis Data Connect official documentation: the portal was not reached; community sources only.
- French injection tariff and any prime: sources conflict; not established.
- Legionella and minimum-temperature requirements for a water heater: not researched.
- Any Go PID or control-loop library: not searched.
- Race detector run on any project: not done.
- Exact evcc line numbers for the cited behaviours: summaries were read, not raw line ranges.

## When looked, and versions

- Local repo: read on 2026-10-31 at worktree commit `ccc92232` (the branch head at start), plus the progress-log commit `82fd5152` added during this run.
- GitHub API metadata (stars, last push, licence): 2026-10-31.
- evcc: `master` on 2026-10-31 (pushed 2026-10-31T16:47:09Z per API). Summaries of `core/site.go`, `core/loadpoint.go`, `core/prioritizer/prioritizer.go`, `api/api.go`, `core/optimizer.md`, `core/site_prioritize_test.go`, `go.mod`, README, LICENSE.
- EMHASS: README on `master`, docs at `/en/latest/` (no version shown). Pushed 2026-10-31.
- Akkudoktor-EOS: `main`, pushed 2026-10-31.
- Shelly API docs: as served on 2026-10-31 (no version number shown on the page).
- Forecast.Solar and Open-Meteo: as served on 2026-10-31.
- Home Connect API docs: index page as served on 2026-10-31; text is long and was read in two chunks.
- Matter 1.4 release page: dated 2024-11-07 on the CSA page.
- Beem: CharlesP44 repo `main`, pushed 2026-10-24.

Some secondary sources are search snippets, not fetched pages. I label them as such in the text.

## Sources

Primary or official:
- evcc repo: https://github.com/evcc-io/evcc ; go.mod: https://github.com/evcc-io/evcc/blob/master/go.mod ; site.go: https://github.com/evcc-io/evcc/blob/master/core/site.go ; loadpoint.go: https://github.com/evcc-io/evcc/blob/master/core/loadpoint.go ; prioritizer: https://github.com/evcc-io/evcc/blob/master/core/prioritizer/prioritizer.go ; site_prioritize_test.go: https://github.com/evcc-io/evcc/blob/master/core/site_prioritize_test.go ; api/api.go: https://github.com/evcc-io/evcc/blob/master/api/api.go ; optimizer.md: https://github.com/evcc-io/evcc/blob/master/core/optimizer.md ; README: https://raw.githubusercontent.com/evcc-io/evcc/master/README.md ; LICENSE: https://raw.githubusercontent.com/evcc-io/evcc/master/LICENSE
- evcc docs: site reference https://docs.evcc.io/en/reference/configuration/site ; sponsorship https://docs.evcc.io/en/sponsorship ; ac-elwa-2 meter https://docs.evcc.io/en/meters/my-pv-ac-elwa-2 ; loadpoints (redirect only) https://docs.evcc.io/en/docs/reference/configuration/loadpoints
- EMHASS: https://github.com/davidusb-geek/emhass ; README https://raw.githubusercontent.com/davidusb-geek/emhass/master/README.md ; docs https://emhass.readthedocs.io/en/latest/
- EMHASS solar diverter: https://github.com/davidusb-geek/emhass-solar-diverter ; README https://raw.githubusercontent.com/davidusb-geek/emhass-solar-diverter/main/README.md
- Akkudoktor-EOS: https://github.com/Akkudoktor-EOS/EOS ; LICENSE https://raw.githubusercontent.com/Akkudoktor-EOS/EOS/main/LICENSE ; README https://raw.githubusercontent.com/Akkudoktor-EOS/EOS/main/README.md
- OpenHEMS: https://openhems.readthedocs.io/en/latest/intro.html ; https://github.com/abriotde/openhems-sample
- OpenEnergyMonitor PV diversion theory: https://docs.openenergymonitor.org/pv-diversion/introduction/choosing-an-energy-diverter.html
- FredM67 PVRouter (Mk2 lineage): https://github.com/FredM67/PVRouter-1-phase
- APPER / xlyric: https://github.com/xlyric/pv-router-esp32 ; README https://raw.githubusercontent.com/xlyric/pv-router-esp32/main/README.md
- Solar-Router-for-ESPHome: https://github.com/hacf-fr/Solar-Router-for-ESPHome ; docs https://hacf-fr.github.io/Solar-Router-for-ESPHome/
- Shelly-dimmer PID gist: https://gist.github.com/mathieucarbou/f9215b311a9018623c1579ab5c2cad7a
- SimpleDerivador: https://github.com/diegargon/SimpleDerivador
- Shelly EM component: https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/EM ; Pro 3EM: https://shelly-api-docs.shelly.cloud/gen2/Devices/Gen2/ShellyPro3EM/
- Beem: CharlesP44 https://github.com/CharlesP44/Beem_Energy ; README https://raw.githubusercontent.com/CharlesP44/Beem_Energy/main/README.md ; ClaraVnk https://github.com/ClaraVnk/home-assistant-beem-energy
- my-PV API news: https://www.my-pv.com/en/news/flexible-control-with-the-my-pv-api
- Matter 1.4 (CSA): https://csa-iot.org/newsroom/matter-1-4-enables-more-capable-smart-homes/
- Forecast.Solar: https://doc.forecast.solar/_export/xhtml/account_models ; https://doc.forecast.solar/api
- Open-Meteo terms: https://open-meteo.com/en/terms
- Home Connect API docs: https://api-docs.home-connect.com/
- RTE Tempo dataset (data.gouv.fr): https://www.data.gouv.fr/en/datasets/tempo-like-supply-contract/
- Go modbus / SunSpec: https://github.com/simonvetter/modbus ; https://github.com/goburrow/modbus ; https://github.com/andig/gosunspec
- Local: `LICENSE` (MPL-2.0), `docs/beem-energy.md`, `docs/pool-pump.md`, `docs/integration-tests.md`, `docs/configuration.md`, `myhome/daemon/solar_*.go`, `internal/myhome/energy/registry.go`, `internal/shelly/scripts/pool-pump.js`, `AGENTS.md`

Secondary (search snippets, or community; not treated as evidence for facts beyond what they are labelled as):
- PvPilot: https://www.pv-magazine.com/2026/02/16/french-startup-launches-universal-home-energy-management-system/
- Fronius Ohmpilot support: https://www.fronius.com/en/help-center/solar-energy/solar-powered-water-heating/support-ohmpilot
- Shelly blog (PV water heating): https://us.shelly.com/blogs/customer-projects/using-photovoltaic-energy-for-water-heating
- Enedis Data Connect (community): https://github.com/consometers/data-connect
- Tempo third-party: https://linuxfr.org/news/tempo-saison-2026-2027-couleurs-et-previsions-en-acces-libre-sans-jeton
- HA Home Connect action docs: https://www.home-assistant.io/actions/home_connect.set_program_and_options/

## Related, not asked

- The repo's own `docs/integration-tests.md` already documents the noise band finding; a grid signal would have a different noise profile, which is untested.
- `pkg/shelly/matter/` exists in the repo. Not examined.
- The `solar_aggregate.go` publish-ordering note becomes a real race as soon as a second source is added.
