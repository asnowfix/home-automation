# `tools/monitor-solar.sh` — observing a live pool-pump run

Samples a running Shelly pump script and records **what it did and why**, to a CSV log. Written for
campaign #401's gate-3 run (solar-driven filtration on `filtration-hiver`), and kept because the
same shape of question recurs: *did the thing I changed cause the behaviour I am seeing?*

## Usage

```bash
# defaults: filtration-hiver, script id 2, broker tcp://192.168.1.2:1883, 5-min samples
./tools/monitor-solar.sh

# a different device / script id / cadence
DEV=mezzanine SID=4 INTERVAL=60 ITERS=60 ./tools/monitor-solar.sh

# a prebuilt CLI instead of `go run ./myhome` (much faster startup per sample)
MH=/path/to/myhome ./tools/monitor-solar.sh
```

| variable | default | meaning |
|---|---|---|
| `DEV` | `filtration-hiver` | target device |
| `SID` | `2` | `pool-pump.js` script id (**4** on `mezzanine`) |
| `BROKER` | `tcp://192.168.1.2:1883` | MQTT broker |
| `MH` | `go run ./myhome` | CLI to call |
| `INTERVAL` | `300` | seconds between samples |
| `ITERS` | `200` | samples before exit |
| `LOG` | `solar-day-<date>.log` | output CSV |

Run it in the background and poll the log; do not sit in the foreground waiting.

## Columns

```
ts,running,mem_used,mem_free,relay,solar_w,stale,note
```

`note` carries events. Hourly, and on **every relay transition**, it also captures device state via a
wrapped `Script.Eval`:

```
[win=648-912 want=true runT=0 out=0]
```

- `win` — the run window in **minutes past midnight** (`648-912` is 10:48–15:12)
- `want` — `F_SOLAR_WANT`: whether solar currently wants the pump on
- `runT` — `STATE.runtimeTodaySec`, **completed intervals only**, so it reads `0` during an open run
- `out` — active output index, `-1` when the script has no opinion

A transition row looks like:

```
15:12:04,,,,TRANSITION,780,False,true->false gap=15741s [win=648-912 want=true ...]
```

## Why it captures *why*, not just *what*

The first version of this script logged only relay state and watts. When the pump started, that was
**not enough to tell a scheduled start from a solar start** — answering it took five ad-hoc
`Script.Eval` calls against the production pump after the fact. Two columns recorded at the time
would have settled it.

Worse, the pump started at 10:49 and nothing said so; the maintainer noticed before the monitor
reported. **A relay transition is the most informative event in any pump experiment.** It is now
printed loudly to stdout as well as logged.

## Two traps this encodes

**A failed RPC is not a dead script.** A dead script still answers `Script.GetStatus` with
`running: false`. An *empty* response means the RPC itself timed out — a comms fault. Conflating them
produced a false "script down" that, under a two-strike abort rule, would have ended a day-long
experiment on an instrument artifact. Empty responses are logged `RPC-FAIL(not counted)` and do not
trigger a restart.

**The run window is rewritten daily.** `handleDailyCheck()` runs at sunrise and recomputes the
morning-start and evening-stop jobs from the forecast. A window read yesterday is wrong today — on
2026-08-23 it was 12:09–15:51, on 2026-08-24 it was 10:48–15:12. Hence the hourly re-read rather than
a value captured at start-up.

## `Script.Eval` discipline

`Script.Eval` is only issued **hourly and on transitions**, never on every sample. It runs code on the
device, consuming stack and heap at a moment the monitor chooses, and init stack headroom on
`pool-pump.js` is thin (see #530, #533). The routine columns come from cheap read-only RPCs.

Every evaluated string is wrapped as `(function(){try{ ... }catch(e){return 'ERR:'+e}})()`. An
unwrapped throw terminates the whole script — it has killed the production pump once and `mezzanine`
twice. Note the body uses **single-quoted** JS strings so the JSON payload needs no escaping; a
double-quoted body silently produced empty results.

## Companion

`tools/solar-probe.py` reads one retained MQTT message (`myhome/energy/solar/available` by default)
and prints `<watts>|<stale>`. Dependency-free by design — this repo vendors no Python MQTT client and
`mosquitto_sub` is not always installed. It never raises; on failure it prints `ERR|<reason>` so a
broker hiccup cannot kill a day-long run.

## Related

`.claude/skills/shelly/references/field-discipline.md` (live-device rules), #401 (the campaign this
was built for), #539 (handover with standing device authorisations).
