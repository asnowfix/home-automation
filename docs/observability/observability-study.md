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
- **A duration-based offline alert would have caught the gate outage about 5 minutes in.** It would
  have fired around 21:48 instead of leaving 12.6 hours of silence. No evidence shows the outage
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
| Thresholds | 5 min for critical devices (gate, pool pump), 30 min for others, battery sleepers excluded, no quiet hours |
| Live access for the study | Read-only permitted |

## 3. Trigger incident: gate devices, 2026-10-31

Terms: "EAP" or "external AP" is the outdoor TP-Link access point; "internal AP" is the indoor Wi-Fi repeater.

Both `fermeture-portail` and `ouverture-portail` are Shelly 1 Mini G3 devices on firmware 2.0.3,
in the same outdoor enclosure.

| Time | Event | Evidence |
|---|---|---|
| 10-28 04:20 | `fermeture-portail` installs firmware; 32 fleet devices update that day | measured |
| 10-30 23:16 to 10-31 16:34 | Repeated short flaps, many shared with 20+ other devices | measured |
| 10-31 21:43:30 | Both report `device.offline` in the same second | measured |
| 11-01 00:00, 06:00 | Daemon restarts; neither device produces any event | measured |
| 11-01 ~10:17 | Enclosure power cycle, per the owner; four devices later show uptime that fits this | owner, measured |
| 11-01 10:17:24 | Both report `device.online` in the same second | measured |
| 11-01 10:19:01 | First logged button press on `fermeture-portail` | measured |

Before the outage, one device had the internal AP then the external AP; the other had the external
AP only. Afterwards, both were set to external first, internal second (owner).

What this establishes:

- The silence of 12.6 h is the only gap over 6.5 h for either device in 60 days (measured).
- The return came 97 s before the first logged button press. Together with reset reason 1 on
  `ouverture-portail`, this points to the power cycle as the restoring action, not the later reset.
  Reset reason 1 meaning power-on is from memory of ESP-IDF, not verified on this firmware. An
  earlier press going unlogged cannot be excluded.
- A shared cause is likely because the timing matched to the second. The cause is unknown.

Measured after recovery, about 11:29: **both devices are on the internal AP's SSID at -92 and -91 dBm,
although the external AP was configured as primary.** All 33 responding devices report
that same SSID, on three BSSIDs with a common vendor prefix. This is one snapshot. Whether the EAP shares that
SSID or is one of those BSSIDs is not known.

Open hypotheses, none proven:

| # | Hypothesis | Test |
|---|---|---|
| H1 | The EAP is not serving these devices, leaving them on a very weak internal AP link | Owner checks the EAP; record BSSID over time |
| H2 | Devices fail to rejoin at very low signal until restarted | Signal and BSSID history on the next event |
| H3 | Firmware of 10-26 raised flap rates: portal devices went from ~1 to ~5 offline events per day | Compare again after 14+ days; the current window is 4 days and includes 10-31 |
| H4 | The `watchdog.js` reboot path made the outage worse, or caused the fleet restart | See section 5 |
| H5 | A power event affected the enclosure circuit | Check with the owner; measure AP uptime |

## 4. Current state of the code and NAS

The NAS is a Raspberry Pi 3B+ (aarch64, 906 MB RAM, about 580 MB available, SD card, measured).
This resolves a discrepancy raised in the alerting notes: the Pi is the NAS.

| Component | State | Evidence |
|---|---|---|
| `myhome/metrics/exporter.go` | Off by default, despite "auto-enabled" in its help text; wired at `myhome/daemon/daemon.go:204` | code |
| Exporter output | Repeats HELP/TYPE per device; `promtool` rejects it; Prometheus 2.53.5 accepted it; 2.42 untested | measured locally |
| Exporter staleness | Offline devices keep serving their last values, with no eviction | code |
| Exporter port | Defaults to 9100, held by node-exporter on the NAS; a bind failure is only logged | measured, code |
| `prometheus-metrics.js` | Crashes when minified (#266); not on the gate devices, which run only `watchdog.js` | issue, measured |
| Prometheus 2.42 on NAS | Active, 44 MB RSS, 602 MB on disk, no device targets, no rules | measured |
| Alertmanager | Referenced by Prometheus config, inactive | measured |
| `prometheus-mqtt-exporter` | Failed; fits #283 | measured |
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
  gate devices (measured). A broker outage of about 5 minutes could reboot the fleet. On 10-31, most
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
(#478), flapping rate.

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
| 0. Gate Wi-Fi | Stabilise gate connectivity | EAP answers obtained; both gate devices on a usable link or a decision recorded |
| 1. See | Collect and store device health; chart it | 7 days of signal, BSSID and uptime data; Pi cost measured |
| 2. Explain | Understand flapping and the `watchdog.js` path | Cause of bursts stated or ruled down; worst four devices fixed or explained; firmware hypothesis tested |
| 3. Alert | Critical and group alerts, banner and email | Test outage alerts within the threshold; noise measured over 14 days |
| 4. Extend | Warning-tier email, WhatsApp, `/metrics`, more rules | Each rule has an observed rate; volume agreed |
| 5. Clean up | Remove unused paths | `prometheus-metrics.js`, packaged exporter and #266, #283 resolved |

Candidate issues, each to be written self-contained:

1. **Gate Wi-Fi investigation:** EAP status, SSID, BSSID; why the devices sit on the internal AP.
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

- Root cause of the gate outage and of the fleet-wide bursts.
- Which BSSID belongs to which AP, and whether the EAP is working.
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
3. Whether phase 0 can proceed in parallel with the code work. Recommended: yes, it is the current
   risk to the gate.
