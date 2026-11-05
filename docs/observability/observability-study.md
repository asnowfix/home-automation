# Observability and alerting study

Status: draft for owner review, 2026-11-01. Not committed. No GitHub issue filed yet.

Evidence labels: **measured** (read from a live system or run), **code** (read in this
repository), **documented** (primary source, see the research notes), **inferred** (reasoned,
not tested), **estimate**.

Research notes, with sources and their own unknowns:

- [research-community.md](research-community.md): platforms, exporters, Shelly MQTT behaviour
- [research-storage-charts.md](research-storage-charts.md): SQLite, TSDBs, exporter defects, charts
- [research-alerting.md](research-alerting.md): rule evaluation, email, WhatsApp, systemd

## 1. Summary

- **Nothing monitors the devices today.** Prometheus runs on the NAS but scrapes only itself and
  node-exporter (measured).
- **A duration-based offline alert would have caught the outage of the two critical devices about 5 minutes in.** It would
  have fired within minutes instead of leaving 12.6 hours of silence. No evidence shows the outage
  could have been predicted.
- **Alerts on today's fleet would be noisy.** There were 115 offline periods of 30 minutes or more
  in 7 days (measured), so Wi-Fi and flapping must be understood first.
- **Recommendation:** collect in the daemon from MQTT, store in SQLite, draw with uPlot, evaluate
  alerts in the daemon, notify through the UI banner and the existing Gmail mailer, and add
  CallMeBot WhatsApp later. Add no device script for metrics.
- **Remove `prometheus-metrics.js` from the plan.** Whether to delete it is decided in phase 3.

## 2. Owner decisions

| Topic | Decision |
|---|---|
| Hosting | The NAS can host the stack |
| Notification | UI banner and email first, WhatsApp if possible |
| Email | Gmail app password over SMTP |
| WhatsApp | Free option first |
| Retention | 1 week by default, configurable |
| Storage | SQLite if the stack is inside the daemon |
| Charts | Native charts first; extensions decided later |
| External dead-man service | Not at this point |
| Thresholds | 5 min for critical devices (the two outage devices, pool pump), 30 min for others, battery sleepers excluded, no quiet hours |
| Live access for the study | Read-only permitted |

## 3. Trigger incident: two critical devices, 2026-10-31

Network details (device identities, access-point models and identifiers, exact times and signal
values) are kept privately by the owner, because this repository is public. Terms used here:
"outdoor AP" is the access point serving the outdoor area; "internal AP" is the indoor Wi-Fi
network, which the outdoor devices use as a fallback.

Two critical Shelly 1 Mini G3 devices, mounted together outdoors, went offline in the same second on
the evening of 2026-10-31 and stayed silent for 12.6 hours (measured). They returned, again in the
same second, after the owner power-cycled their installation. Neither produced any event across the
two daemon restarts in between. Before the outage, one device had the internal AP as primary and the
outdoor AP as fallback; the other had the outdoor AP only. Afterwards, both were set to outdoor AP
first, internal AP second (owner).

What this establishes:

- The 12.6 h silence is the only gap over 6.5 h for either device in 60 days (measured).
- The power cycle, not the later reset of one device, appears to be what restored them. They were
  back before the first logged button press, and one device's reset reason indicates power-on. That
  reset-reason meaning is from memory of ESP-IDF, not verified on this firmware.
- A shared cause is likely because the timing matched to the second. The cause is unknown.

What happened after recovery (measured):

| Period | Connected to | Signal | Offline events |
|---|---|---|---|
| First ~8 h after recovery | Internal AP (fallback) | Very weak, below -90 dBm | 8 and 16 in one afternoon |
| Following 3.5 days | Outdoor AP (primary) | Good, about -55 dBm | 0 |

The devices moved to the outdoor AP without rebooting. This is a single observation, but it is the
strongest signal found so far: **a critical device sitting on its fallback AP flapped constantly,
and the same device on its primary AP did not flap at all.**

Open hypotheses, none proven:

| # | Hypothesis | Test |
|---|---|---|
| H1 | The outdoor AP was not serving the devices, leaving them on the weak fallback | Owner checks the AP; record each device's AP over time |
| H2 | Devices fail to rejoin at very low signal until restarted | AP and signal history on the next event |
| H3 | Firmware of 2026-10-26 raised flap rates | Compare again after 14+ days; the first window was 4 days and included 10-31 |
| H4 | The `watchdog.js` reboot path made the outage worse, or caused the fleet restart | See section 5 |
| H5 | The power cycle also restarted the outdoor AP, which booted after the devices | Owner confirms how the AP is powered |

## 4. Current state of the code and NAS

The NAS is a Raspberry Pi 3B+ with under 1 GB of RAM and an SD card (measured). This resolves a
discrepancy raised in the alerting notes: the Pi is the NAS.

| Component | State | Evidence |
|---|---|---|
| `myhome/metrics/exporter.go` | Off by default, despite "auto-enabled" in its help text; wired at `myhome/daemon/daemon.go:204` | code |
| Exporter output | Repeats HELP/TYPE per device; `promtool` rejects it; Prometheus 2.53.5 accepted it; 2.42 untested | measured locally |
| Exporter staleness | Offline devices keep serving their last values, with no eviction | code |
| Exporter port | Its default port is already taken on the NAS; a bind failure is only logged | measured, code |
| `prometheus-metrics.js` | Crashes when minified (#266); not on the two critical devices, which run only `watchdog.js` | issue, measured |
| Prometheus on NAS | Installed, but monitors no device and has no rules | measured |
| Alerting and MQTT exporter on NAS | Installed but not working; the exporter failure fits #283 | measured |
| Email | `myhome/notify/gmail.go` and the `Mailer` interface already exist; failed digests are logged and dropped | code |
| Event hook | `broadcastFn` at `myhome/daemon/daemon.go:307` already fans events out to services | code |
| Online state | Only stored as event rows, with no in-memory state or deduplication (`internal/myhome/shelly/gen2/listener.go:241`) | code |
| MQTT broker | Embedded in the daemon (mochi), so a daemon restart also disconnects every device | code |

Related issues: #61, #247, #266, #283, #478, #495, #517, #580, #429.

## 5. Flapping analysis

Pairing each `device.offline` with the next `device.online`, last 7 days (measured):

| Offline for | Count |
|---|---|
| Under 5 s | 1,916 |
| 5 s to 1 min | 291 |
| 1 to 5 min | 53 |
| 5 to 30 min | 74 |
| 30 min or more | 115 |

- **Four devices dominate:** `lumiere-escalier-exterieur` (92 per day), `lumieres-jardin` (32),
  `spots-piscine` (27), `lumiere-pool-house-pilier` (22). Their rates did not rise after 10-28.
- **Fleet-wide bursts:** up to 25 devices report offline in the same few seconds, usually back
  within 2 s. Cause unknown: AP, broker and daemon are all candidates.
- **Signal alone does not explain it:** `lampe-buffer-escalier` is at -94 dBm with about 5 events per
  day, and `arrosage` is at -72 dBm with about 4 (one snapshot each). The community research also
  found reports of flaps with good signal, and no evidence that signal decline predicts failure.

Two inferences that affect the design:

- **Silence is the signal for a device already offline.** During the outage, two daemon restarts
  produced no event at all from either device (measured). The embedded broker appears to lose its
  state on restart, so a device absent at restart stays silent. The evaluator must therefore treat a
  known device with no recent online message as offline, and persist state across restarts.
- **`watchdog.js` may be a source of events, not only a safeguard.** It reboots a device after 5
  failed 60-second MQTT checks (`internal/shelly/scripts/watchdog.js:20`, code), and it runs on the
  critical devices (measured). A broker outage of about 5 minutes could reboot the fleet. On 10-31, most
  devices restarted in software around 10:30 (from uptime), and online events without matching
  offline events appeared at 10:02, 10:08, 10:24 and 10:34, which is what a broker restart would look
  like. This fits, but the journal has rotated and no direct proof exists. Reboots are not recorded in
  `events.db` unless they appear as `scheduled_restart`. During a long Wi-Fi outage, the same path
  could also reboot a device repeatedly. Note this is a live-device mechanism: read it with the
  `shelly` skill before changing it.

Reset reason 4 on `radiateur-salon-old-front-door`, about 5.4 h before the survey, may be a crash
(reason code meaning from memory). Tracking reset reasons makes this visible.

## 6. Proposed architecture

```
Shelly devices (no new script)
   | MQTT: <id>/online, <id>/events/rpc      | low-rate RPC: Wifi.GetStatus, Sys.GetStatus
   v                                          v
embedded broker -> myhome daemon: collector -> device state (memory + SQLite)
                                       |                |
                     metrics.db (SQLite, 7 days)   alert evaluator
                                       |                |-> UI banner (existing notice flow)
                     /api charts -> uPlot in UI         |-> Gmail mailer (existing, add queue)
                     /metrics (client_golang)           |-> WhatsApp via CallMeBot (later)
                        -> optional later: Prometheus, Grafana or Perses

systemd: Type=notify + WatchdogSec + OnFailure -> independent small check script -> email
```

### Collection

- Use the daemon's existing MQTT traffic for online state and event data.
- Shelly's docs do not say whether RSSI and free RAM are pushed periodically (documented gap), so
  capture real traffic first. Poll only what is missing, starting at every 5 minutes (estimate) and
  staggered across devices.
- First series: online, RSSI, SSID, BSSID, uptime, reset reason, RAM free, heap, script running,
  plus daemon self-metrics.
- Exclude sleeping battery devices from polling and offline rules.

### Storage

- A new SQLite file, `metrics.db`, next to `events.db`, as a plain relative filename by default.
- Key: `WITHOUT ROWID`, primary key `(ts, series_id)`, time first and no secondary index. Measured
  on a workstation: about 91 to 104 MiB for 4 million samples per week, and 56 to 72 MiB of
  application writes per day. A series-first key or secondary index writes about 3.7 to 4 GiB per
  day.
- Write once a minute in one transaction. Delete expired rows in batches, with a configurable
  retention defaulting to 7 days.
- Re-measure size, CPU and disk writes on the Pi. SD-card wear was not established.
- Keep events and metrics separate: `events.db` records what happened; `metrics.db` records values
  over time.

### Charts

uPlot: MIT, about 22 KB gzipped, native time axis, embedded through `go generate`. Its last release
was March 2025, so Dygraphs is the fallback. Adding an asset must follow the `go generate` rules in
`CLAUDE.md`.

### `/metrics`

Replace the exporter with `client_golang`, on a free port, with a per-device `up` metric and
staleness removal. Grafana (512 MB evaluation floor, about all the free memory on this Pi) and Perses
(needs Prometheus) remain optional extensions. Neither was tested on this hardware.

### Alerting

An evaluator in the daemon, because Prometheus and Alertmanager add two processes and offer nothing
essential at this scale, and their semantics are simple to copy.

- States: inactive, pending, firing, resolved, persisted in SQLite so a restart does not reset them.
- `for` duration per tier; resolve message; re-notification interval; 10-minute startup grace period
  (estimate, to tune).
- Group fleet-wide events into one notification, and distinguish "everything dropped" (likely AP,
  broker or power) from "one device dropped".
- Email queue with bounded retry, so an internet outage does not lose alerts. This satisfies the
  internet-optional rule because the UI banner stays local.

First rules:

| Rule | Tier | Delivery at launch |
|---|---|---|
| Critical device offline over 5 min | Critical | Banner + email |
| Other device offline over 30 min | Warning | Banner only until volume is acceptable |
| Group outage: many devices offline together | Critical | Banner + email, one message |
| Daemon failed or hung | Critical | Independent check, email |

Later rules, enabled only once a measured rate supports them (per the rule in `CLAUDE.md`):
weak signal, unexpected AP or BSSID change, repeated reboots, crash reset reasons, stopped scripts
(#478), flapping rate. The strongest candidate so far is "critical device connected to its fallback AP", from the single observation in section 3.

### WhatsApp

CallMeBot is free and for personal use, with no published limits or SLA, and it places the message
text and API key in a query string. So send only short non-sensitive text. It would come behind the
existing `Mailer`-style pluggable interface, as a secondary channel. The official Cloud API charges
for business-initiated alerts, so it does not meet "free option first". Telegram or ntfy remain
alternatives if WhatsApp stops being a requirement. Terms were read through a summarising tool, so
re-check them before implementing.

### Failure coverage

| Failure | Detected by |
|---|---|
| Device down | Evaluator |
| Daemon crash or hang | systemd watchdog plus independent script |
| Internet down | Banner still works; email queued |
| NAS or power down | **Not covered**, by owner decision. Nothing alerts |
| Broker down | Daemon self-check, because the broker is in the daemon |

Whether `RuntimeMaxSec` with `Restart=always` triggers `OnFailure` is unresolved. Test it, so that
the scheduled 6-hour restart does not send false alerts.

### Degraded mode

The observability stack is advisory. A failure in it must never block device control or daemon
startup, and no device function depends on it. Metrics pause while the daemon is down.

## 7. Campaign epic (draft)

Phases are ordered so that each one produces evidence for the next.

| Phase | Goal | Exit condition |
|---|---|---|
| 0. Critical-device Wi-Fi | Explain why the critical devices fell back from their primary AP | Cause found or recorded as unknown; devices on their primary AP |
| 1. See | Collect and store device health; chart it | 7 days of signal, BSSID and uptime data; Pi cost measured |
| 2. Explain | Understand flapping and the `watchdog.js` path | Cause of bursts stated or ruled down; worst four devices fixed or explained; firmware hypothesis tested |
| 3. Alert | Critical and group alerts, banner and email | Test outage alerts within the threshold; noise measured over 14 days |
| 4. Extend | Warning-tier email, WhatsApp, `/metrics`, more rules | Each rule has an observed rate; volume agreed |
| 5. Clean up | Remove unused paths | `prometheus-metrics.js`, packaged exporter and #266, #283 resolved |

Candidate issues, each to be written self-contained:

1. **Critical-device Wi-Fi:** why the two critical devices fell back from their primary AP, and whether that AP can fail unnoticed.
2. **MQTT capture on hardware:** what Shelly pushes and when, whether `online` is retained, and the
   memory and network cost of one status RPC. Read-only.
3. **`metrics.db` store:** schema, batching, retention, config options in all four required files.
4. **Device health collector:** with staggered polling and per-device online state.
5. **Native charts:** uPlot asset and a device-health view.
6. **Record reboots and reset reasons:** so restarts stop being invisible in `events.db`.
7. **Flapping investigation:** bursts, worst four devices, 14-day firmware comparison.
8. **`watchdog.js` reboot review:** rate from data, and behaviour during long outages. Touches live
   devices, so capture before-state first.
9. **Alert evaluator:** persisted state, grouping, grace period, tests that simulate a restart.
10. **UI alert banner:** on the existing notice flow.
11. **Email queue and retry:** on the existing Gmail mailer, with timeouts.
12. **Daemon watchdog:** systemd settings and independent check, including the `OnFailure` test.
13. **`/metrics` rewrite:** `client_golang`, free port.
14. **WhatsApp notifier:** CallMeBot behind the notifier interface.
15. **Clean-up:** `prometheus-metrics.js`, exporter packaging, and #61, #266, #283, #247 updates.

Dependencies: 3 and 4 before 5 and 7. 2 before 4. 6 before 8. 4 before 9. 9 before 10, 11, 14.

## 8. Unknowns

- Root cause of the critical-device outage and of the fleet-wide bursts.
- Why the critical devices left their primary AP, and why they returned without rebooting.
- Periodic or on-change push of RSSI and free RAM; whether `online` is retained. Needs a capture.
- Pi RAM, CPU and SD write costs of the proposed store and collector. Only workstation figures exist.
- Cost of status RPCs on the devices with the least free memory.
- `RuntimeMaxSec` and `OnFailure` interaction.
- CallMeBot limits and reliability.
- Why the four non-surveyed devices did not respond, and two devices with no stored hostname were
  skipped (`lumieres-jardin` and `spots-piscine`, assumed from their absence).
- Whether any metric predicts failure. Treat this as an experiment for phase 2, not a promise.

## 9. Decisions needed

1. Approve the architecture, particularly in-daemon alerting and SQLite.
2. Approve filing the epic and child issues on GitHub.
3. Whether phase 0 stays a separate phase. The critical devices have been stable on their primary AP since 2026-11-01, so it could shrink to one issue run alongside phase 1.
