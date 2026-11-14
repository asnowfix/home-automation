# Solar router research — progress log

Task: survey PV surplus diverters / HEMS prior art for a Go (or Shelly JS) load-allocation design.
Output: `docs/solar-router-research.md` (final). This file is the append-only working log.
Worktree: `/home/fix/GIT/home-automation/.claude/worktrees/feature+solar-router`, branch `worktree-feature+solar-router`.
Started: 2026-10-31.

## Step 1 — local repo survey (done)

Read / grepped:
- `docs/beem-energy.md` — Beem integration spec. Says REST only (PnP kit), no official API, `wattHour` field is instantaneous W despite its name, Beem Battery MQTT channel reserved (`GridW`), poll 60 s.
- `docs/monitor-solar.md` — not a solar doc; it is the Shelly-side monitor doc (not used).
- `docs/configuration.md` ~L530-545 — solar aggregator config: `solar.stale_after` default 5m; `beem.poll_interval` default 60s.
- `docs/pool-pump.md` ~L352-400 — solar hysteresis (#405): start 500 W / stop 200 W defaults, start/stop delays, soft target (`solar-min-turnover`=5) and hard ceiling (`solar-max-turnover`=7).
- `docs/integration-tests.md` ~L600-655 — live test on 2026-08-30 (filtration-hiver, Pro1): solar feed alternated 360↔420 W every ~45 s; start hold resets on ANY single sample below threshold (no debounce/averaging). Default 500 W is structurally unreachable against that feed; lowered to 300 W to pass. Key design lesson: the start-side hysteresis has no averaging/debounce.
- `myhome/daemon/solar_source.go` — `SolarSource` interface (`Name()`, `Subscribe(ctx) <-chan SolarReading`). Only implementation: `solar_beem_source.go` (wraps `pkg/beem.Watcher.PowerCh`).
- `myhome/daemon/solar_aggregate.go` — sums last-known reading per source, drops sources older than `staleAfter`, republishes retained `myhome/energy/solar/available` with `{available_w, ts(unix s), sources[]}` on EVERY reading (no periodic republish). Documented caveat: two sources can publish out of order (no lock across publish); harmless with one source.
- `myhome/daemon/solar_rpc.go`, `internal/myhome/solar.go`, `internal/myhome/energy/registry.go` — static claimers registry (`pool-pump` only, `Register(name, deviceID)`), `solar.claimerslist` RPC, best-effort live read for pool pump with 3 s timeout. Explicitly NOT an arbitration engine. Its test file registers a `water-heater` claimer name as test data only; no water-heater code exists.
- `internal/shelly/scripts/pool-pump.js` ~L2347-2520 — device-side subscription to `myhome/energy/solar/available`, staleness from payload `ts` (not receipt time), hysteresis `checkSolarHysteresis()`, 30 s tick, calls own `doStart`/`doStop`. Stale → solarWant(false) → schedule alone decides.
- `internal/shelly/scripts/heater.js` — a Kalman-filter house-heating controller that uses Open-Meteo (no key). Not a water heater. Open-Meteo already used by pool-pump and heater (precedent for the internet-optional rule).
- `pkg/beem/client.go`, `types.go` — endpoints `api-x.beem.energy/beemapp/user/login`, `/beemapp/box/summary`, `/beemapp/devices`; `boxSummary` fields `wattHour` (instantaneous W), `totalDay`, `totalMonth`, `lastAlive`, `lastProduction`, `power` (nameplate), `weather` (always null in captures).
- Grep for water heater / dishwasher / Home Connect / Linky / Tempo / Modbus / SunSpec / Shelly EM / Pro3EM / evcc: NO implementation exists. `pkg/shelly/schedule/jobs.json` has an "Heures Creuses EDF" description string (a schedule label only).
- `docs/beem-energy.md` open-question list: Beem Battery upgrade (GridW), Prometheus metrics (#247), speed-adaptive solar (#248).

Ruled out for the local context: nothing yet — local survey only establishes what exists.

## Step 2 — external survey (in progress)

Planned order: evcc (Go) → mk2PVrouter / Routeur Solaire → OpenEnergyMonitor PV diverter → myPV Elwa → EMHASS → Akkudoktor-EOS → Home Assistant solar-diverter / Solcast / Shelly diverter examples → openHEMS / SolarAssistant / ESPHome → Fronius Ohmpilot, Victron → Beem (CharlesP44, ClaraVnk) → "Matter" France → Forecast.Solar / Open-Meteo / Solcast → Shelly EM & Pro 3EM API → Home Connect API (deferrable start) → RTE Tempo open data → Go libs (goburrow/modbus, simonvetter/modbus).

## Step 2 — external findings so far (2026-10-31)

Read (primary where reachable; source noted in the final file):
- evcc (github.com/evcc-io/evcc): MIT per GitHub API and LICENSE file; README says sponsor-required components are excluded from MIT. Go module `github.com/evcc-io/evcc`, go.mod has `grid-x/modbus` (replaced by evcc-io/modbus) and `andig/gosunspec`. core/site.go `sitePower()` formula; core/loadpoint.go `pvMaxCurrent` enable/disable timers (Enable.Delay 1 min, Disable.Delay 3 min defaults, minCurrent 6 A, maxCurrent 16 A); core/prioritizer/prioritizer.go = sums lower-priority demand, no distribution, no hysteresis, no timer; API interfaces in api/api.go (Charger, Meter, Dimmer, Curtailer, CircuitLoad...).
- evcc docs: sponsorship page ties tokens to commercial EV chargers and "simple switch-sockets and plugins" (wording: "The same applies to simple switch-sockets and plugins").
- core/optimizer.md: MILP cost minimisation, 15-min slots, multi-loadpoint coordination not described.
- mk2pvrouter lineage: FredM67/PVRouter-1-phase LGPL-2.1 (GitHub API), refactored from Robin Emley's sketch; burst-fire at zero crossing (forum summaries only; primary About files 404).
- xlyric/pv-router-esp32: licence "Other" per GitHub API; README says CC BY-NC-SA 4.0; ESP32 TTGO; inputs SCT013 / Shelly EM / Enphase; per-load scheduler.
- OpenEnergyMonitor PV diversion page: burst mode vs phase control vs PWM comparison (fetched from /introduction/choosing-an-energy-diverter.html).
- Shelly Pro 3EM docs: no sign convention stated; storage at 1-minute intervals; active_power_change event fires at >=10 W and 5%.
- EMHASS (davidusb-geek/emhass): MIT, Python, LP per README; emhass-solar-diverter (MIT, PID to 0 W export, Shelly Dimmer 0-10V to LCTC/LSA regulator).
- Akkudoktor-EOS: Apache-2.0 per fetched LICENSE file; GitHub API reports NOASSERTION (conflict to record). GENETIC optimizer per README.
- OpenHEMS (abriotde/openhems-sample GPL-3.0; docs say optimisation layer over HA; RTE Tempo supported; EMHASS strategy).
- Shelly-based HA projects: mathieucarbou gist (PID, Shelly Dimmer + LSA, Kp=1 starting), HACF Solar-Router-for-ESPHome (GPL-3.0, moved; progressive/ON-OFF routing, triac/SSR/relay outputs), SergioRius Node-RED PID flow.
- myPV Elwa: cloud REST API in BETA (my-pv news, 2024-10-07); local JSON/control only from community posts; official local docs request-only.
- Fronius Ohmpilot: inverter sends setpoint; Modbus RTU via RS485 recommended; PWM per phase.
- Beem: CharlesP44/Beem_Energy MIT (pushed 2026-10-24) — README says Battery is "entièrement supportée", EnergySwitch partial, PnP not mentioned; no official API docs; developer.beem.energy does not resolve (DNS ENOTFOUND). ClaraVnk MIT, last push 2025-05-21.
- "Matter France": could not identify a French provider with a Matter/API story. Found PvPilot (Toulouse HEMS, pv-magazine 2026-02-16) — no API stated. Matter 1.4 (CSA, 2024-11-07) adds Device Energy Management, Water Heater Mode, Solar Power device types (CSA release page).
- Forecast.Solar: public tier free, no key, 12 calls per IP per 60 min rolling, +1 day, 1 h resolution; data updated at earliest every 15 min.
- Open-Meteo: free tier non-commercial only; <10,000 calls/day, 5,000/hour, 600/min; CC-BY 4.0.
- Home Connect: 1000 requests/day per client+user; 50/min; 5 program starts/min; events channel is SSE ("Opening an SSE stream counts as 1 request"); dishwasher "Start In Relative Option" label present.
- RTE Tempo: RTE data portal API needs account+token (secondary sources); data.gouv dataset "Tempo Like Supply Contract" published by RTE, licence not specified on dataset, past colours open; forecasts only via third parties (calendrier-tempo.fr, CC BY 4.0 per their comment).
- Enedis Data Connect: consumption load curve 30-min resolution (community repos only; official portal not reached).
- Go libs: simonvetter/modbus MIT (pushed 2026-07-28); goburrow/modbus BSD-3 (last push 2024-06-23); andig/gosunspec BSD-3 (pushed 2026-07-05).
- French tariffs (EDF OA surplus price): sources conflict; NOT established.
- SolarAssistant: MQTT API for monitoring and settings; licence not established.
- ruled out / not usable: "Matter France" identity unknown; mk2pvrouter.co.uk pages 404 at tested URLs; OpenEnergyMonitor MK2 About .txt 404.

Next: write docs/solar-router-research.md, then commit both files.
