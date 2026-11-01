# Research: how a small Go home-automation daemon should evaluate alerts and deliver notifications

## What was asked

How should the `myhome` daemon (Go, Raspberry Pi 3B+, ~40 Shelly devices, must work offline,
restarts every 6 h via `RuntimeMaxSec=21600`) evaluate alerts (device offline for N minutes,
with pending/firing/resolved state, flap suppression, fleet-wide grouping, startup grace,
re-notify and resolve messages) and deliver them (UI banner, Gmail SMTP with app password,
a free WhatsApp route, pluggable notifier), and how to watch the daemon without external
services?

## Conventions used in this file

- **(verified)** or **[V]**: read from a primary source (a local file at a stated line, or a fetched
  official page or spec) during this research. Each carries its source.
- **(inference)** or **[I]**: my reasoning from verified facts. Not measured. Labelled so it is not
  mistaken for a finding.
- **unverified** or **[U]**: not established. Listed in section G.
- Quotations from web pages came through a summarising fetch tool. See G.21 before quoting externally.
- "When looked": all local reads are against worktree branch `worktree-feature+observability`,
  HEAD `33be2480` (2026-11-01). Web sources are dated where the page shows a date; the access
  date is 2026-11-01.

---

## A. Local code: how events and device-online state flow (verified)

### A.1 Where `device.online` / `device.offline` are created

- **gen2 (Shelly Gen2+)**: the listener subscribes to `+/online` on MQTT
  (`internal/myhome/shelly/gen2/listener.go:39-40`). `handleOnline` (`:241-266`) maps payload
  `"true"` to `device.online`, anything else to `device.offline` (`:248-251`), stamps
  `Ts = time.Now().Unix()` at receipt (`:253`), and calls `service.Record` (`:262`).
  Component is `"mqtt"`, severity `"info"` (`:254-260`).
- **gen1**: `internal/myhome/shelly/gen1/listener.go:128-143`. Topic `shellies/<id>/online`;
  payload `"1"` or `"true"` maps to `device.online`, else `device.offline`.
- There is **no** deduplication of repeated identical states. Every message on the topic
  produces a row. `handleOnline` does not look at the previous state for that device.
- **No in-memory online state exists in the device manager.** `myhome/devices/impl/manager.go`
  contains no reference to online/offline status. `DeviceView` (`internal/myhome/ui/template.go:55-76`)
  has no online field. The only persisted representation of online state is the event rows.
  [V: grep of `*.go` for `online|offline|IsOnline|Online` returned only the listeners above,
  an occupancy LAN check, sfr/router code, and the pool script status struct.]

### A.2 Storage and the write path

- `myhome/events/storage.go:16-27` `Event` struct (ts, received_at, device_id, component,
  event, severity, data).
- `myhome/events/storage.go:39-47` `Query` struct: filters are DeviceIDs/DeviceID, EventType,
  Severity, Since (duration), Limit, Offset. There is **no** "current state of device X"
  query; a state-based alert needs its own query or in-memory state.
- `myhome/events/storage.go:97-107` table `events` has
  `UNIQUE (device_id, component, event, ts)`. Indexes on ts, (device_id,ts), (event,ts),
  (severity,ts) (`:108-111`).
- `myhome/events/storage.go:143-156` `Record` uses `INSERT OR IGNORE`. Because `ts` is whole
  seconds for `device.online`/`offline`, two identical rows in the same second collapse to one.
  Repeats in later seconds are kept.
- `myhome/events/storage.go:71` `SetMaxOpenConns(1)`; WAL mode (`:73`).
- `myhome/events/storage.go:257` `OnDurationSec` already exists: a precedent for an
  on/off duration query over the events table.
- `myhome/events/service.go:53-67` `Record` writes to storage, then calls `broadcast(e)`.
- `myhome/events/service.go:28-46` hourly purge by retention (default 90 days,
  `myhome/daemon/run.go:77`, flag `--events-retention`).

### A.3 Fan-out after a row is recorded (daemon wiring)

- `myhome/daemon/daemon.go:307-313` `broadcastFn` runs three things for every recorded event:
  `sseBroadcaster.BroadcastEvent(e)`, `noticeSvc.OnEvent(d.ctx, e)` (if set),
  `poolNotices.OnEvent(d.ctx, e)`. This is the natural hook point for a rule evaluator that
  reacts to events (see section E).
- `myhome/daemon/daemon.go:315-326` events storage and service created (gated by
  `options.Flags.EnableEventsService`). Failure to open storage is non-fatal (`:317-320`).
- `myhome/daemon/daemon.go:346-364` notice service wiring, gated by
  `EnableNoticeService` and requires events + occupancy services.
- `myhome/daemon/daemon.go:397` `d.dm.WithEventService(eventsSvc, eventsTracker)`.
- `myhome/daemon/daemon.go:406-409` gen2 listener is constructed and started, only if
  `eventsSvc != nil && eventsTracker != nil`.
- `myhome/daemon/daemon.go:570` `ui.Start(...)` receives `sseBroadcaster`, `eventsSvc`,
  and `accountsRegistry`.

### A.4 Existing notice / email code (important: this is not greenfield)

- `myhome/notice/service.go:57-72` `Start`: a daily digest loop. A failed send is logged
  (`:66-70`) and **dropped**. There is no queue and no retry.
- `myhome/notice/service.go:78-94` `OnEvent`: the motion rule. Only acts on
  `motion.detected`.
- `myhome/notice/service.go:133-149` `SendDigest`: queries the last 24 h of `notice`
  severity events (limit 1000) and calls `mailer.Send`.
- `myhome/notice/config.go:25-29` defaults: night 22:00 to 06:00, digest hour 8.
- `myhome/notify/mailer.go:21-23` `Mailer` interface: `Send(ctx, subject, body) error`.
- `myhome/notify/mailer.go:46-54` `New`: returns a no-op mailer when `From` is empty.
- `myhome/notify/mailer.go:51-53` default timeout 10 s when the context has no deadline.
- `myhome/notify/gmail.go:27-92` the SMTP sender, hand-written on `net/smtp`:
  - dial with context (`:34-37`), deadline applied to the whole conn (`:43-45`);
  - STARTTLS only if the server advertises it (`:53-57`);
  - PLAIN AUTH only if the server advertises AUTH and a username is set (`:59-64`);
  - `MAIL FROM`, `RCPT TO` per recipient, `DATA`, `Quit` (`:66-91`).
  - No retry, no persistence, no backoff, no idempotency key.
- `myhome/daemon/run.go:87-88` flags `--smtp-host` (default `smtp.gmail.com`) and
  `--smtp-port` (default 587).
- `myhome/daemon/run.go:267-275` SMTP username, password, from, and to come from
  viper (environment or config file, keys `smtp.username` etc.), **not** from CLI flags.
  The comment says they are "never CLI flags". The password is therefore not in the process
  argument list. (I did not read `/var/lib/myhome/.env`; the existence of the mechanism is
  established only from `linux/systemd/myhome.service:31` `EnvironmentFile=-/var/lib/myhome/.env`.)
- `myhome/daemon/daemon.go:55-66` `reportingMailer` wraps the Mailer and reports each send
  outcome into the accounts registry under the name `smtp`.

### A.5 Accounts registry and the existing UI "status" panel

- `internal/myhome/accounts/registry.go:18-24` `Status{Name, Enabled, LastOK, LastAttempt,
  LastError}`.
- `internal/myhome/accounts/registry.go:51-63` `Report(name, err)`: records last outcome only.
  It keeps no history and no "consecutive failures" counter.
- `internal/myhome/accounts/registry.go:78-88` `Snapshot()` returns a sorted copy.
- `internal/myhome/ui/server.go:106` route `/htmx/accounts`. `server.go:80` route `/events`
  (SSE).
- `internal/myhome/ui/htmx.go:75` `toAccountRows`; `:107` `AccountsPanel`; `:116` renders it.
- `internal/myhome/ui/static/index.html:104-118` the Accounts section. It is loaded via
  `hx-get="/htmx/accounts" hx-trigger="load"` (`:109-110`). It is loaded once on page load
  and is **not** refreshed by SSE or polling. [V: the attribute `hx-trigger="load"` only.]
- `internal/myhome/ui/static/index.html:49` the `<body>` carries Alpine `x-data="app()"`.
  `index.html:52-57` is a `hero is-light is-small` section at the top of the page, which is
  where a banner would normally sit.

### A.6 Browser live updates

- `internal/myhome/ui/static/index.html:463` `new EventSource('/events')`, handlers for
  `connected` (`:465`), `sensor-update` (`:469`), `device-update` (`:479`). It does **not**
  subscribe to `eventlog`.
- `internal/myhome/ui/events_template.go:227-233` a second `EventSource('/events')` listens
  for `eventlog` and renders rows.
- `internal/myhome/ui/sse.go:31` `sseSlowClientMaxSkips = 10`; `:66` per-client channel buffer
  is 10. `:91-135` a client that fails to accept 10 consecutive messages is **evicted**
  (channel closed). A client that misses messages because it was evicted is not replayed.
  [V: code; the consequence for a banner is an inference: a banner fed only by SSE can miss
  a transition while the tab is slow or reconnecting. It should re-fetch state on reconnect.]

### A.7 MQTT session behaviour (matters for "restart causes device.online bursts")

- `myhome/mqtt/client.go:209` `SetCleanSession(false)` (persistent session, "to receive
  retained messages reliably").
- `myhome/mqtt/client.go:208` `ResumeSubs(true)`; `:213-215` keepalive 30 s, ping timeout 10 s,
  connect retry every 5 s.
- `myhome/mqtt/client.go:157` the reconnect interval is taken from
  `options.Flags.MqttReconnectInterval`.
- `myhome/mqtt/client.go:411-445` the watchdog goroutine performs a **periodic
  disconnect/reconnect/re-subscribe** when `reconnectInterval > 0`, explicitly to "refresh
  retained messages" (`:418`, comment `:439`: "with CleanSession=false, broker will deliver
  all retained messages").
- `myhome/ctl/options/options.go:43` `MQTT_RECONNECT_INTERVAL = 2 * time.Hour`.
  `myhome/daemon/run.go:59` exposes it as `--mqtt-reconnect-interval`, default that constant.
- **[I] Consequence:** every 2 h reconnect re-subscribes `+/online` and, if the broker
  retains the per-device `online` topic (to be checked against Shelly docs, section C), the
  broker re-delivers the retained state for every device. `handleOnline` then records a fresh
  `device.online` row per device, because it does not dedupe. If so, `device.online` counts
  include re-delivered retained state, not only real transitions. This is an inference from
  code. It is not measured. A query of `events.db` would settle it, but that is a live system
  and I did not touch it (see section F).

### A.8 Daemon process supervision (what the code does, not what systemd does)

- `linux/systemd/myhome.service:8` `Type=simple`.
- `:10-11` `Restart=always`, `RestartSec=10`.
- `:23-24` `StartLimitIntervalSec=300`, `StartLimitBurst=5`.
- `:27` `RuntimeMaxSec=21600` (the 6 h restart).
- `:31` `EnvironmentFile=-/var/lib/myhome/.env`.
- **No `WatchdogSec`, no `NotifyAccess`, no `OnFailure=`** in the unit. grep of the repo for
  `WatchdogSec|OnFailure|NotifyAccess` finds none in `linux/`.
- The daemon does not call `sd_notify`: there is no `go-systemd` or `sdnotify` in any
  `go.mod` I checked. Service management uses `github.com/kardianos/service` v1.2.4
  (`myhome/daemon/go.mod:11`), which handles install/start/stop, not readiness or watchdog
  pings.

### A.9 Other local facts that bear on alerting

- `myhome/devices/impl/manager.go:644-696` `runDeviceRefreshJob`: one Gen2 device refreshed
  per tick (`:685`). It is started at `:386`. On a `GetAllDevices` error it **logs and
  returns** (`:663-664`), which ends the refresh goroutine for the life of the process.
  [V: code. This is adjacent to alerting, not asked. It means a single DB error silently
  stops refreshes.]
- `myhome/devices/impl/manager.go:698-730` `runReconciliationLoop` re-applies MQTT/NTP
  config over HTTP to every Gen2 device on an interval (`:424`). This means HTTP reachability
  of devices is exercised by a separate path. It is not used for online state.
- `myhome/metrics/exporter.go:156-168` the `/metrics` handler serves a cache of
  **device-published** Prometheus text, relayed from MQTT. It is not a daemon-side Prometheus
  registry. Port 9100 by default (`myhome/ctl/options/options.go:23`).
- `hlog/hlog.go` and `myhome/daemon/daemon.go` import `kardianos/service`; no
  systemd-notify library is used.

---

## B. Rule evaluation: in-daemon vs Prometheus + Alertmanager vs Grafana alerting

### B.1 What the daemon has today (local, verified)

- There is no state-based evaluator. Online state exists only as `device.online` /
  `device.offline` rows (section A.1). Absence of a row is not an event, so "offline for N
  minutes" needs either a per-device timer or a periodic sweep, fed by the event stream
  (`broadcastFn`, `myhome/daemon/daemon.go:307-313`).
- `OnDurationSec` (`myhome/events/storage.go:257`) already computes on/off durations from the
  events table, and deduplicates consecutive "on" events. It is a precedent for duration logic
  over stored events.
- The daemon already has the two parts a rule service needs: an event hook
  (`notice.Service.OnEvent`, `myhome/notice/service.go:78`) and a scheduler loop
  (`notice.Service.Start`, `:57`).

### B.2 Prometheus alerting semantics (verified, primary)

Source: https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/ (fetched
2026-11-01; the page shows no version, footer "2014-2026").

- `for`: "The optional `for` clause causes Prometheus to wait for a certain duration between
  first encountering a new expression output vector element." Elements that are active but not
  firing are "in the pending state." Without `for`, "Alerting rules without the `for` clause will
  become active on the first evaluation."
- Reset: an alert "will deactivate on the first evaluation where the condition is not met,
  assuming any optional `for` duration described above has been satisfied." The page does **not**
  say what happens to a pending alert whose condition clears before `for` elapses (section G).
- `keep_firing_for`: keeps an alert firing "for the specified duration" after the condition was
  last met; the page says this can prevent "flapping alerts".
- `ALERTS` series: "set to `1` as long as the alert is in the indicated active (pending or firing)
  state." The page does not describe `ALERTS_FOR_STATE`.

Source: https://prometheus.io/docs/prometheus/latest/command-line/prometheus/ (fetched 2026-11-01):

- `--rules.alert.for-outage-tolerance` (default `1h`): "Max time to tolerate prometheus outage for
  restoring "for" state of alert. Use with server mode only."
- `--rules.alert.for-grace-period` (default `10m`): "Minimum duration between alert and restored
  "for" state." It applies only to alerts whose `for` is greater than the grace period. **This is
  about restoring pending state after an outage, not a startup suppression window.**
- `--rules.alert.resend-delay` (default `1m`): minimum wait before resending an alert to
  Alertmanager.

Comparison for a 6-hour restart: per these flag descriptions, Prometheus restores `for` state after
an outage shorter than `for-outage-tolerance`. The daemon has no equivalent mechanism. This is a
comparison, not a finding about the daemon's behaviour.

### B.3 Alertmanager grouping, repeat and resolve (verified, primary)

Source: https://prometheus.io/docs/alerting/latest/configuration/ (fetched 2026-11-01; the page
states no version for these route options).

- `group_wait` (default `30s`): "How long to wait before sending the first notification for a new
  group of alerts."
- `group_interval` (default `5m`): "How long to wait before sending subsequent notifications for an
  existing group of alerts after group_wait."
- `repeat_interval` (default `4h`): "How long to wait before repeating the last notification."
- `group_by`: "The labels by which incoming alerts are grouped together." Example from the page:
  "multiple alerts coming in for cluster=A and alertname=LatencyHigh would be batched into a single
  group." This is the verified mechanism for "one notification for a fleet-wide event".
- `send_resolved` is a receiver setting, not a route setting. The page lists
  `Discord, Mattermost, Webhook: default = true` and `Email, Slack, WeChat: default = false`.
- `email_config` (same page): `require_tls` and `send_resolved` (default `false`). The page says
  "Go does not support unencrypted connections to remote SMTP endpoints." Fields `smarthost`,
  `auth_username`, `auth_password` are present, each defaulting to a `global.smtp_*` value.

Alertmanager's own state persistence across restarts was not established (section G).

### B.4 Grafana alerting (verified in part)

Source: https://grafana.com/docs/grafana/latest/alerting/fundamentals/alert-rules/state-and-health/
(fetched 2026-11-01; `/latest/`, no version shown).

- "The Error state is triggered when the alert rule fails to evaluate its query or queries
  successfully."
- "The No Data state occurs when the alert rule query runs successfully but returns no data points
  at all."
- Pending period: "Set the pending period to 0 to skip the Pending state and enter Error
  immediately." NoData and Error can be set to "Set No Data state", "Set Error state", "Set
  Alerting state", "Set Normal state" or "Keep last state".
- Not on that page: "keep firing for", and definitions of the Normal, Alerting and Pending
  states. Not established (section G).

Hardware (verified): https://grafana.com/docs/grafana/latest/setup-grafana/installation/ gives
"Minimum recommended memory: 512 MB" and "Minimum recommended CPU: 1 core" (fetched 2026-11-01,
`/latest/`). Grafana also needs a data source that holds the state (for example Prometheus), so it
adds a second service.

### B.5 Prometheus storage constraints (verified, partial)

Source: https://prometheus.io/docs/prometheus/latest/storage/ (fetched 2026-11-01).

- "Prometheus stores an average of only 1-2 bytes per sample."
- "It is strongly recommended to use a local filesystem for reliability." "NFS filesystems
  (including AWS's EFS) are not supported."
- Memory: "The current block for incoming samples is kept in memory and is not fully persisted."
- No RAM-per-series figure on that page. A Pi 3B+ sizing claim is **not** established here.

### B.6 Comparison as evidence (not a recommendation)

| Option | What the docs verify | What it needs here | Gap against this project |
|---|---|---|---|
| In-daemon evaluator | Nothing external. Daemon already has an event hook, a scheduler loop and SQLite | New per-device state, persisted; timers | Must implement `for`, grouping, repeat and resolve itself. Restart survival depends on whether the online topic is retained (section G) |
| Prometheus + Alertmanager | `for`, `keep_firing_for`, `group_by`, `group_wait`, `repeat_interval`, `send_resolved`, `for`-state restore after outage | A scraped metric for device online state. The existing exporter serves device-published Prometheus text, not a daemon-side online gauge (`myhome/metrics/exporter.go:156-168`), so the daemon would have to expose one | Two more long-running processes on constrained hardware. RAM per series not established |
| Grafana alerting | Error and NoData states, pending period | Grafana server (512 MB minimum recommended) plus a data source | Heaviest. No advantage over Alertmanager for the email and UI-banner delivery the owner chose |

Observation (for the owner to weigh, not a plan): the owner's requirement for duration-keyed alerts
with fleet grouping maps onto `for`, `group_by` and `repeat_interval`. Those are verified semantics,
and an in-daemon evaluator would have to reproduce them. Scrape-based evaluation can miss
transitions shorter than the scrape interval. That is an inference from how scraping works, not
something the fetched docs state.

---

## C. Email: net/smtp vs maintained libraries; Gmail app password and limits; timeouts; retry

### C.1 Go standard library and libraries (verified)

- `net/smtp` status: "The smtp package is frozen and is not accepting new features." Source:
  https://pkg.go.dev/net/smtp (fetched 2026-11-01; page shows `go1.27.3`). The page does not call
  it deprecated.
- `net/smtp` PlainAuth: "PlainAuth will only send the credentials if the connection is using TLS or
  is connected to localhost." Otherwise "authentication will fail with an error, without sending the
  credentials." (same page)
- `net/smtp` documents **no** context or timeout support (absence on the pkg.go.dev page). The repo
  works around this by setting a deadline on the raw connection (`myhome/notify/gmail.go:43-45`).
- `github.com/wneessen/go-mail` (MIT; https://pkg.go.dev/github.com/wneessen/go-mail, fetched
  2026-11-01):
  - Versions from https://pkg.go.dev/github.com/wneessen/go-mail?tab=versions (fetched 2026-11-01):
    v0.8.1 Jul 9, 2026; v0.8.0 Jul 4, 2026; v0.7.3 May 12, 2026; v0.7.2 Sep 29, 2025; v0.7.1 Sep 26,
    2025. Only the v0 path is listed; no v2.
  - README statements (as extracted by the fetch tool): "Makes use of contexts for a better control
    flow and timeout/cancelation handling"; a `WithTimeout(timeout)` option with a 15 s default;
    "Explicit STARTTLS support with different policies", default TLS policy mandatory; PLAIN auth
    supported, with `ErrPlainAuthNotSupported` when the server does not offer it.
  - Stability: still v0. The page says "When a project reaches major version v1 it is considered
    stable."
  - Go support: "go-mail will always support the same set of Go versions that the Go team actively
    maintains."
- Not checked: `gomail.v2` and other libraries. Their maintenance status is not established.

### C.2 Existing project code (verified)

- `myhome/notify/gmail.go:27-92` is hand-written on `net/smtp`. Context deadline, or a 10 s default
  (`myhome/notify/mailer.go:51-53`); dial with context (`gmail.go:34-37`); deadline on the whole
  connection (`:43-45`); STARTTLS only if advertised (`:53-57`); PLAIN auth only if advertised and a
  username is set (`:59-64`). No retry, queue or backoff.
- `myhome/notice/service.go:66-70`: a failed digest send is logged and dropped.
- Observation: `go-mail` would replace the transport but not the drop-on-failure behaviour. Whether
  it has built-in retry was not established.

### C.3 Gmail SMTP settings and app passwords (verified, with a conflict)

- Google Help, SMTP settings (https://support.google.com/mail/answer/7104828, fetched 2026-11-01,
  footer "©2026 Google"): Outgoing (SMTP) `smtp.gmail.com`, "Requires Authentication: Yes", "Port for
  TLS/STARTTLS: 587". "If you use Gmail with your work or school account, check with your
  administrator for the correct SMTP configuration." Port 465 is **not** on that page.
- App passwords (https://support.google.com/accounts/answer/185833, fetched 2026-11-01): "To create
  an app password, you need 2-Step Verification on your Google Account." "App passwords can only be
  used with accounts that have 2-Step Verification turned on." Blockers listed include Advanced
  Protection and 2-Step Verification "set up only for security keys".
- **Conflicting Google statements.** https://support.google.com/mail/answer/7126229 ("Add Gmail to
  another email client", fetched 2026-11-01) says: "Gmail no longer supports third-party apps or
  devices which require you to share your Google username and password." Under "(Optional) Step 3:
  Check for password issues" it says: "App passwords aren't recommended and are unnecessary in most
  cases." That page does not mention SMTP. I found no Google page announcing that app-password SMTP
  access will stop working. A third-party guide (developer.nylas.com, surfaced by search) claims
  Google finished removing plain-password access in May 2025. That claim is **unverified**, and it
  concerns plain passwords, not app passwords.
- The owner's decision (Gmail SMTP with an app password) matches the SMTP settings page. The owner
  should read the 7126229 wording before relying on app passwords long term. This is reported as a
  conflict between two Google pages, not resolved here.

### C.4 Gmail sending limits (verified)

- Consumer accounts, https://support.google.com/mail/answer/22839 (fetched 2026-11-01): the error
  appears when "you send an email to a total of more than 500 recipients in a single email", or for
  "more than 500 emails sent in a day". Retry: "you should be able to send emails again within 1 to
  24 hours." The page does not say the limits apply only to personal accounts; it points to a
  separate page for work or school accounts.
- Workspace, https://knowledge.workspace.google.com/admin/gmail/gmail-sending-limits-in-google-workspace
  (reached by redirect from support.google.com/a/answer/166852; the page shows "Last updated
  2026-10-26 UTC"): 2,000 messages per day per user (standard); 100 recipients per message via SMTP,
  POP or IMAP. "users can't send new messages for up to 24 hours" after a limit. "Sending limits can
  change without notice."
- Inference: the owner brief's 200-670 `device.offline` rows per day, sent as one email each, would
  exceed 500 messages per day on a consumer account. Grouping is therefore required. The brief's
  counts were not re-measured here.

### C.5 Timeouts, queueing and retry when offline

- Timeouts: 10 s default (`myhome/notify/mailer.go:51-53`) and a connection deadline
  (`gmail.go:43-45`). Verified in code.
- Queue and retry: **nothing in the repo persists or retries a failed send** (verified:
  `notice/service.go:66-70`). Neither the `net/smtp` page nor the fetched `go-mail` page describes a
  persistent queue. A queue and retry policy would have to be added by the project (inference).
- Offline: SMTP to `smtp.gmail.com` needs internet. The project rule (CLAUDE.md, "Resilience —
  internet-optional") requires the local path, such as the UI banner, to work without it. That is a
  requirement, not a finding about the code.

---

## D. Free WhatsApp options, other free routes, and a pluggable notifier

### D.1 CallMeBot (free, third-party, WhatsApp)

Sources: https://www.callmebot.com/blog/free-api-whatsapp-messages/ (post dated "March 20, 2021" on
the page; fetched 2026-11-01); https://www.callmebot.com/ (homepage, fetched 2026-11-01).

- Terms: "Note: The Free API is only for personal use." The same note appears on several later posts
  dated 2021 to 2024 that the fetch tool listed (for example "May 1, 2024"). These are excerpts, not the
  full posts. No formal terms-of-service page was found (section G).
- Not allowed on the free API, per the post: "Send messages to others", "Send messages to Groups",
  "Send messages from your whatsapp number", "Send Images", "Send Buttons", "Send documents",
  "Receive the answers". Paid alternatives are mentioned without details.
- Activation: add the bot number to contacts; send it the text "I allow callmebot to send me
  messages."; the API key comes back by WhatsApp. "If you don't receive the ApiKey in 2 minutes,
  please try again after 24hs."
- Request format (verbatim example from the post):
  `https://api.callmebot.com/whatsapp.php?phone=+34123123123&text=This+is+a+test+from+CallMeBot&apikey=1234567890`.
  The HTTP method is not stated; a query-string GET is implied.
- Limits and reliability: no rate limit, quota, uptime or SLA is published on either page (verified
  absence). The homepage says "This is a hobby and the all the donations are used to maintain the
  servers on the cloud."
- Privacy (inference from the verified URL format): the message text and the API key both travel in
  the query string to a third-party host. Any proxy or server log that records URLs would record them.
  The service operator can read the message text. The key allows sending as that bot to the
  activated number, so it is a credential.

### D.2 WhatsApp Business Cloud API (official, free tier does not cover alerts)

Sources: pricing page https://developers.facebook.com/docs/whatsapp/pricing (the page itself states
"This page reflects pricing updates that became effective as of October 1, 2026 by Messaging account
timezone."; fetched 2026-11-01). Template fundamentals, reached from the moved page
https://developers.facebook.com/docs/whatsapp/cloud-api/guides/send-message-templates →
https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/overview
(fetched 2026-11-01, no date shown). Get-started page
https://developers.facebook.com/docs/whatsapp/cloud-api/get-started (fetched 2026-11-01).

Free tier (pricing page, as extracted):
- "Each business phone number has one free tier of 1,000 delivered service messages per month."
- "Unused service messages in the free tier do not roll over to the next month."
- Service messages are the non-template kind. Template messages are "Marketing", "Utility" or
  "Authentication". Utility is defined as "template messages for transactional or informational
  objectives."

Utility messages in a customer-service window:
- Free from July 1, 2025 for "Utility messages sent in response to users" (open window).
- The pricing page says the pricing type for these changed from `free_customer_service` to `regular`
  "as of October 1, 2026, so they are now charged." **This is a change in the last month; check the
  live page before relying on it.**

Template rules (verified from the templates overview page):
- "Template messages are the only type of message that can be sent to WhatsApp users outside of a
  customer service window."
- "Templates must have a status of `APPROVED` before they can be sent in template messages."
- "Templates are automatically reviewed upon creation or after editing." The page does not say
  whether Meta or an automated process performs the review.
- "Each template must be categorized as authentication, marketing, or utility." No restriction on
  promotional content in utility templates appears on the page.
- The 24-hour customer-service window is **not defined** on the fetched page. It links to a "Send
  messages" page. Duration not established here (section G), though it is commonly described as 24
  hours.

Consequence for alerts (inference from the verified text): an alert is business-initiated, so it has
to be a template outside an open window. Templates are not in the free service-message tier. Each
such alert is therefore a charged utility message. The per-message rate table by country was not
extracted.

Requirements (verified from the get-started page): a Facebook or managed Meta account, developer
registration, and "WhatsApp-enabled device for sending and receiving test messages". A WhatsApp
Business Account "is created during setup". A permanent token should be created through a system user.
Not on the page: whether business verification is required, and whether test numbers cost money.

Policy (verified via a French-locale mirror of the Business Messaging Policy,
https://whatsappbusiness.com/fr/policy/, page shows "Dernière mise à jour : 23 septembre 2026"; the
English clause was quoted by the fetch tool): "You may only contact people on WhatsApp if: (a) they
have given you their mobile phone number or username; and (b) you have received opt-in permission from
the recipient confirming that they wish to receive" messages. The page does not name the Cloud API or
an approved access route.

### D.3 Unofficial WhatsApp clients (not free of risk; not recommended)

The WhatsApp Terms of Service page (https://www.whatsapp.com/legal/terms-of-service, shows "Effective
Date: January 4, 2021"; fetched 2026-11-01) contains these clauses, as extracted:
- "create software or APIs that function substantially the same as our Services"
- "through automated or other means, access, use, copy, adapt, modify"
- "reverse engineer, alter, modify, create derivative works from, decompile, or extract code from our
  Services"
- "involve sending illegal or impermissible communications such as bulk messaging, auto-messaging,
  auto-dialing, and the like"
- "involve any non-personal use of our Services unless otherwise authorized by us"

A reading of these clauses against unofficial clients is an inference. The Terms are dated 2021 on the
page, which may not be current. The Business Terms page did not expose the relevant text (section G).

### D.4 Other free routes (verified where stated)

- **Telegram Bot API** (https://core.telegram.org/bots/faq, fetched 2026-11-01): "By default, bots are
  able to message their users at no cost." Rate limits: "In a single chat, avoid sending more than one
  message per second." "In a group, bots are not be able to send more than 20 messages per minute."
  Overflow gives "429 errors". The FAQ says to spread bulk notifications over longer intervals.
  Bot-chat privacy properties were not checked (section G).
- **ntfy** (https://docs.ntfy.sh/ and https://docs.ntfy.sh/config/, fetched 2026-11-01): "ntfy lets you
  send push notifications to your phone or desktop via scripts from any computer." The docs have a
  "Self-hosting" section. "Note that topic names are public, so it's wise to choose something that
  cannot be guessed easily." On the public server, "everyone can read and write to any topic (this is
  how ntfy.sh is configured)". Self-hosted defaults: each visitor's request bucket is 60 requests,
  refilled one per 5 s (`visitor-request-limit-burst`, `visitor-request-limit-replenish`). Apps come
  "via Google Play, App Store or F-Droid".
- **Shoutrrr-supported services** (below) include Gotify, Matrix, Pushover and Email. Pushover pricing
  was not checked.

### D.5 Privacy and ToS risk, side by side

| Route | ToS fit (verified) | Privacy (verified or inferred) | Reliability (verified) |
|---|---|---|---|
| CallMeBot | "only for personal use"; no sending to others, groups or images | Text and key in query string to a hobby third party (inferred logging risk) | No SLA, no published limits; activation may fail with a 24 h wait |
| WhatsApp Cloud API | Official. Opt-in required. Business-initiated needs approved templates | Processed by Meta under business terms. Costs per utility template since Oct 1 2026 | Official API; no SLA stated on the pages read |
| Unofficial WhatsApp client | Clauses above prohibit "software or APIs that function substantially the same" and automated access | Not assessed. Account-ban risk (inferred) | Not assessed |
| Telegram Bot API | Free to message users; rate limits | Bot-chat privacy not checked | Documented limits |
| ntfy.sh (public) | Open read/write to any topic | Topic name is the only secret and is public | Self-hosting possible |
| ntfy (self-hosted on LAN) | Not a third-party terms issue | Stays on the LAN; phone app still needs internet to reach the server if the phone is away (inferred) | Depends on the host, see section F |

### D.6 Recommendation for the WhatsApp channel (for the owner's decision, not a plan)

Of the zero-cost WhatsApp routes, only CallMeBot needs no Meta business account and no per-message
billing. The official Cloud API's free tier does not cover business-initiated alerts (D.2). So the
owner's "free option first" points to CallMeBot, with conditions:
- It is personal use only, which fits a self-notification. Keep message text to device names and states
  (no sensitive detail). This is consistent with the URL-query exposure in D.1.
- It has no published SLA, so it can only be a secondary channel. Email and the UI banner stay primary,
  as the owner decided.
- Its delivery state should be visible in the accounts panel, like the existing SMTP status
  (`myhome/daemon/daemon.go:63-65`).

If WhatsApp itself is not required, Telegram or ntfy are the free routes with documented ToS.

### D.7 Pluggable notifier: shoutrrr and the existing in-repo pattern

- Shoutrrr, containrrr repository (verified via GitHub REST API, https://api.github.com/repos/containrrr/shoutrrr,
  fetched 2026-11-01): `archived: false`, `pushed_at: 2026-07-01T20:02:57Z`, `updated_at:
  2026-10-31T10:56:50Z`, `license.spdx_id: MIT`, 1,710 stars, 81 open issues. Project page
  https://github.com/containrrr/shoutrrr: "Notification library for gophers and their furry friends."
- Shoutrrr fork, nicholas-fedor (verified via GitHub REST API,
  https://api.github.com/repos/nicholas-fedor/shoutrrr, fetched 2026-11-01): `fork: true`,
  `parent.full_name: containrrr/shoutrrr`, `archived: false`, `pushed_at: 2026-11-01T10:06:52Z`,
  MIT, 190 stars.
- Services listed on the v0.8 docs page (https://containrrr.dev/shoutrrr/v0.8/services/overview/,
  fetched 2026-11-01; page says "You're not viewing the latest version"): Bark, Discord, Email, Gotify,
  Google Chat, IFTTT, Join, Mattermost, Matrix, Ntfy, OpsGenie, Pushbullet, Pushover, Rocketchat,
  Slack, Teams, Telegram, Zulip Chat, Generic Webhook; plus Logger under "Specialized services".
  **WhatsApp is not listed.** The fork's service list was not checked.
- Email URL format on that page: `smtp://username:password@host:port/?from=fromAddress&to=recipient1[,recipient2,...]`.
  Credentials sit inside the URL. For this project that is an observation about secret handling, not a
  verdict.
- Existing pattern in this repo (verified): `myhome/notify/mailer.go:21-23` defines
  `type Mailer interface { Send(ctx, subject, body string) error }`, with a no-op implementation when
  email is off (`:46-50`). Its own comment calls it "provider-agnostic". A pluggable notifier can follow
  this shape without a new dependency. Shoutrrr would be a dependency, not a replacement for this code.

---

## E. Where alert evaluation and a UI banner would most naturally attach (file:line)

Verified against the worktree (HEAD `33be2480`). These are attachment points, not a design.

**Event source (the only path for online transitions)**
- gen2: `internal/myhome/shelly/gen2/listener.go:39-40` (subscribe `+/online`), `:241-266` (`handleOnline`),
  `:262` (`service.Record`).
- gen1: `internal/myhome/shelly/gen1/listener.go:128-143` (`eventSvc.Record`).
- Both call `events.Service.Record` (`myhome/events/service.go:53-67`), which writes to SQLite and then
  calls `broadcast(e)`. Hook point for transitions: that `broadcast` is the `broadcastFn` closure
  (`myhome/daemon/daemon.go:307-313`).

**Evaluator (new consumer, same shape as the existing notice service)**
- Wiring precedent: variables declared before the closure (`daemon.go:297-306`), assigned later
  (`:346-364`), called from `broadcastFn` (`:307-313`).
- Shape precedent: `myhome/notice/service.go` has a constructor (`:44-53`), an event hook (`OnEvent`,
  `:78-94`), a scheduler loop (`Start`, `:57-72`), and a read of recent rows through
  `s.events.Store().Query(...)` (`:134-138`). `events.Service.Store()` is at `myhome/events/service.go:49-51`.
- Startup rebuild: query `events.Storage.Query` (`myhome/events/storage.go:158`), `events.Query` struct at
  `:39-47`.
- Timers: a per-device or periodic sweep needs a clock. `notice.Service.untilNextDigest` (`:122-128`) is a
  local time-of-day pattern, not a duration pattern.
- Must dedupe repeated `device.online` rows, since `handleOnline` writes one per message (A.1).

**Notification sending**
- `notify.Mailer` (`myhome/notify/mailer.go:21-23`) behind `reportingMailer` (`myhome/daemon/daemon.go:55-66`),
  which writes outcome to `accounts.Registry.Report` (`internal/myhome/accounts/registry.go:51-63`).

**UI banner**
- Data route: pattern is `mux.HandleFunc("/htmx/accounts", htmxHandler.AccountsPanel)`
  (`internal/myhome/ui/server.go:106`), handler `AccountsPanel` (`internal/myhome/ui/htmx.go:107-120`),
  renders `h.accountsRegistry.Snapshot()` (`:116`). Routes are registered in `server.go:80-108`.
- Page placement: the hero section `static/index.html:52-57`; the Accounts section at `:104-118`
  (`hx-get="/htmx/accounts" hx-trigger="load"`, `:109-110`). A banner would sit between the hero and the
  first content section (`:78`), or before Accounts at `:104`.
- Live update: the browser already holds `new EventSource('/events')` at `static/index.html:463`.
  Server side is `internal/myhome/ui/sse.go` (`BroadcastEvent` at `:171`, `broadcast` at `:91-135`,
  `ServeHTTP` at `:183`). A new event name would be one more `broadcast` call and one more listener.
- Caveat for an SSE-only banner: the client channel holds 10 messages (`sse.go:66`), and a client
  failing 10 consecutive sends is evicted (`sse.go:31`, `:91-135`). A banner should re-fetch its state
  on connect rather than rely on the stream alone (inference from the eviction code).
- Existing status for the banner to reuse: `accounts.Registry.Snapshot()`
  (`internal/myhome/accounts/registry.go:78-88`), with per-account `LastOK` and `LastError`. That
  already reports the SMTP outcome.
- There is **no** online state in `DeviceView` (`internal/myhome/ui/template.go:55-76`) or in the device
  manager (`myhome/devices/impl/manager.go`), so any banner that lists offline devices needs the
  evaluator's state, not an existing field.

**Project rules that touch the attachment points (CLAUDE.md)**
- New RPC methods need four steps (`Verb` constant, request/response types, `signatures` map,
  `RegisterMethodHandler`); the rule is in the project CLAUDE.md "RPC System" section.
- "Never create a separate MQTT subscription" for RPC; the daemon's existing subscriptions are the path.

---

## F. Watching the daemon without external services

### F.1 What the unit and the code do today (verified)

- `linux/systemd/myhome.service`: `Type=simple` (`:8`), `Restart=always` (`:10`), `RestartSec=10`
  (`:11`), `StartLimitIntervalSec=300` and `StartLimitBurst=5` (`:23-24`), `RuntimeMaxSec=21600` (`:27`),
  `EnvironmentFile=-/var/lib/myhome/.env` (`:31`; the file was not read).
- No `WatchdogSec`, `NotifyAccess` or `OnFailure=` in `linux/` (grep of the repo).
- The daemon does not call `sd_notify`. No systemd notify library appears in any `go.mod`. Service
  management uses `github.com/kardianos/service` v1.2.4 (`myhome/daemon/go.mod:11`), which handles
  install, start and stop.
- Device-side watchdog, `internal/shelly/scripts/watchdog.js` (verified code, read at this commit):
  - `numberOfFails: 5` and `retryIntervalSeconds: 60` (`:20-21`), with the comment "5 retries connceting
    to the server before rebooting the device".
  - `checkMqttConnection` (`:85-116`) counts consecutive `MQTT.isConnected()` failures; at 5 it calls
    `attemptReconnect` (`:62-71`), which calls `Shelly.Reboot` unless `SHARED_STATE.rebootLock` is set.
  - **Consequence (inference from the code, not run):** if the MQTT broker is unreachable for about 4 to 5
    minutes, every device that carries this script reboots, and repeats after each boot. The owner brief's
    fleet-wide bursts of up to 25 `device.offline` rows could therefore be device reboots rather than network
    flaps. This needs checking against `events.db` (section G).
  - The project CLAUDE.md says the auto-setup applies only to devices lacking `watchdog.js` as script 1, so
    this script is expected on most devices. Not verified on each device.

### F.2 systemd behaviour (verified, primary)

Source: https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html (page header shows
systemd 262; fetched 2026-11-01).

- `Type=notify`: "it is expected that the service sends a "READY=1" notification message via sd_notify(3)
  or an equivalent call".
- `WatchdogSec=`: "The service must call sd_notify(3) regularly with "WATCHDOG=1" (i.e. the "keep-alive
  ping")." "If the time between two such calls is larger than the configured time, then the service is
  placed in a failed state" and "it will be terminated with SIGABRT (or the signal specified by
  WatchdogSignal=)."
- `RuntimeMaxSec=`: "Configures a maximum time for the service to run." "If this is used and the service
  has been active for longer than the specified time it is terminated" and "put into a failure state."
  The page does not say that this triggers a restart.
- `Restart=`: "Configures whether the service shall be restarted when the service process exits, is
  killed, or a timeout is reached." Its timeout list names "start, reload, and stop operation timeouts"
  and the watchdog deadline. It does not list `RuntimeMaxSec`.

Source: https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html (fetched 2026-11-01):

- `OnFailure=`: "A space-separated list of one or more units that are activated when this unit enters the
  "failed" state." (Added in version 201.) The fetched text does not say whether this applies across
  `Restart=` cycles.
- **Open point (section G):** the systemd text does not settle whether `RuntimeMaxSec` expiry plus
  `Restart=always` ever puts the unit in the "failed" state that `OnFailure=` watches. Consequently, an
  `OnFailure=` hook could fire every 6 hours, or never. This needs a test on an isolated unit, not the live
  service.

Go library (verified): https://pkg.go.dev/github.com/coreos/go-systemd/v22/daemon (fetched 2026-11-01;
page lists v22.7.0, published Jan 27, 2026; Apache-2.0). Functions: `SdNotify(unsetEnvironment, state)`,
`SdNotifyMonotonicUsec()` (v22.7.0), `SdWatchdogEnabled(unsetEnvironment)`. `SdNotify` returns `(false, nil)`
when `NOTIFY_SOCKET` is unset. `SdWatchdogEnabled` returns `(0, nil)` when the watchdog is not enabled or
the process is not the watched PID, and otherwise the interval. The page says processes should ping at half
that interval.

### F.3 Independent check options (what each can and cannot see)

Verified inputs:
- Shelly Gen2 `Mqtt.GetStatus` exposes "True if the device is MQTT connected, false otherwise" in the
  `connected` field (https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt, fetched 2026-11-01).
- The default topic prefix is the device ID, and the online topic is `<shelly-id>/online` (same page).
- The daemon's ports (CLAUDE.md "Ports"): 6080 dev UI, 80 under systemd, 6060 pprof, 9100 metrics.
  The metrics target `192.168.1.2:9100` is in `README.md:675`.

Options, with the detection limit of each (analysis, not run):
1. **A second LAN host** (not the host running the daemon or the broker) probes the daemon's HTTP UI and the
   broker port. Detects: daemon down, broker down, host down. Cannot detect: anything on the observer host
   itself. Its alert path must avoid the monitored host. Email needs internet; the UI banner and a LAN-local
   notification do not.
2. **A device-side check** using `watchdog.js`-style logic. Detects: broker unreachable from that device
   (via `MQTT.isConnected()` or `Mqtt.GetStatus`). Cannot detect: the daemon being down while the broker is up,
   because the device does not see the daemon. The device heap is tight (CLAUDE.md: "about 7 KB of free heap")
   and a device alert needs a route the device can reach. Notification from a device is a separate design
   question not answered here.
3. **Daemon self-report** (`WatchdogSec` with `sd_notify`, or a heartbeat row in `events.db`). Detects: the
   daemon process hung. Cannot detect: the daemon never starting, or the host being down, because then nothing
   runs to send the ping.

### F.4 What can and cannot be detected when the NAS itself is down

Premise to confirm first: the repo gives two host signals. The owner brief says the daemon runs on a Raspberry
Pi 3B+. `linux/README.md:30` shows a Raspberry Pi kernel (`6.6.51+rpt-rpi-v8 ... aarch64`). The project
CLAUDE.md "NAS operational facts" describes `myhome.service` on the NAS (192.168.1.2), and
the broker is `tcp://192.168.1.2:1883` (CLAUDE.md, `CONTRIBUTING.md:102`, `README.md:790`). `README.md:675`
scrapes `192.168.1.2:9100`. These cannot all be right for one host. **Not established which host runs the
daemon and which runs the broker.** The answer below holds for either layout, as stated.

If the NAS (broker and, per CLAUDE.md, the daemon) is down:
- **Detectable from elsewhere on the LAN:** that the NAS is unreachable (ping or HTTP from another host).
  This needs an observer that is not the NAS (F.3 option 1).
- **Not detectable by the daemon:** the daemon cannot run, so it cannot send anything. This is a property of
  any daemon-side alerting. It is not a gap that a rule engine inside the daemon can close.
- **Devices keep their local function** (project resilience rule). Their MQTT watchdog reboots them after about
  4 to 5 minutes without a broker (F.1, inference). The fleet will look like a burst of offline and online
  rows when the NAS returns.
- **Not detectable at all, without internet or a second site:** an internet-delivered alert (email, WhatsApp,
  Telegram, ntfy.sh) cannot reach the owner if the internet is also down. The owner declined an external
  dead-man service, so this residual gap is accepted in the brief and is recorded here only as a limit.

---

## G. What could not be established

Each item is a gap, not a finding that something is absent. Where a source was not reached, that is said.

1. **Whether the Shelly Gen2 `<id>/online` message is retained, and whether the built-in LWT is retained.**
   The Gen2 MQTT page (https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt) does not mention
   "retain". The Gen2 script API documents `MQTT.publish(topic, message[, qos[, retain]])` with "Default: false"
   (https://shelly-api-docs.shelly.cloud/gen2/Scripts/APIs/MQTT). The Gen1 MQTT page says the LWT "is retained
   on user configuration (if the Retain flag is set)" and "we do not recommend using retained MQTT messages"
   (https://shelly-api-docs.shelly.cloud/gen1/#shelly-family-mqtt). A Shelly forum thread from the gen1 era
   reports non-retained online messages (https://shelly-forum.com/thread/4800-mqtt-last-will-and-testament-not-honoring-the-retain-flag-for-online-status,
   surfaced by search, not read in full). **This decides whether the restart burst (41 `device.online` rows
   per restart) can come from retained redelivery. It is open.**
2. **Whether the 2-hour periodic MQTT re-subscribe creates `device.online` rows.** Inference from
   `myhome/mqtt/client.go:411-445` plus `myhome/ctl/options/options.go:43` (2 h), and from `handleOnline` not
   deduplicating (A.1). Settling it needs a read-only count over `events.db` on the live host. That is a live
   system, so it was not done.
3. **Root cause of the 200-670 `device.offline` rows per day and the 25-device bursts.** The owner brief gives
   these figures. They were not re-measured. The watchdog reboot path (F.1) is a candidate explanation, not a
   finding.
4. **Whether `OnFailure=` fires when `RuntimeMaxSec` and `Restart=always` cycle the unit** (F.2). Needs a test on
   an isolated unit.
5. **Prometheus RAM per series, and whether Prometheus plus Alertmanager fit a Pi 3B+.** The storage page gives
   disk figures but no RAM-per-series number. A separate research note in this folder
   (`docs/observability/research-storage-charts.md`) states "906 MB RAM, ~580 MB available". I saw that line only in a grep result and did not read or verify the note.
6. **Alertmanager's own persistence across restarts** (storage path, notification log). Not on the pages fetched.
7. **Grafana "keep firing for" and the Normal/Alerting/Pending state definitions.** Not on the page fetched.
8. **A "startup grace" feature in Alertmanager or Grafana.** None found on the pages fetched. Prometheus's
   `--rules.alert.for-grace-period` is about restoring pending state, not startup suppression (B.2).
9. **What a pending alert does when its condition clears before the `for` duration.** Prometheus says it
   "deactivate[s] on the first evaluation where the condition is not met", with the `for` caveat; the
   pending-to-inactive path is not spelled out.
10. **Gmail app passwords and SMTP over the long term.** Two Google pages conflict in tone (7126229 vs 7104828 and
    185833). No Google page announcing removal of app-password SMTP was found. A third-party claim about May 2025
    (developer.nylas.com) is unverified (C.3).
11. **The WhatsApp 24-hour customer-service window.** The fetched template page does not define it. Duration not
    established here.
12. **Per-message utility template rates by country**, and whether business verification is needed for the Cloud
    API. Not extracted (D.2).
13. **The WhatsApp Business Terms.** The page fetched (https://www.whatsapp.com/legal/business-terms) only links to
    two documents. Their text was not read, so clauses on unofficial clients under the Business Platform terms
    are not established.
14. **CallMeBot terms of service and reliability.** No formal terms or SLA page was found. Only the 2021 post and
    homepage were read (D.1). Dates on the excerpts are the posts' own dates.
15. **Telegram bot privacy properties** (whether bot chats are end-to-end encrypted). Not checked.
16. **ntfy self-hosted behaviour on a Pi, and whether the phone app uses a third-party push service.** The ntfy
    docs fetched did not say which push service apps use (D.4).
17. **Shoutrrr's fork service list** (nicholas-fedor). Only the containrrr v0.8 docs were read (D.7).
18. **Whether go-mail retries failed sends.** The pkg.go.dev page was read; its retry behaviour was not.
19. **Which host runs the daemon** (F.4). Conflicting repo signals.
20. **Gen1 topic shape.** The Gen1 doc gives `shellies/<shellymodel>-<deviceid>/online`
    (https://shelly-api-docs.shelly.cloud/gen1/#shelly-family-mqtt). The gen1 listener parses `shellies/<id>/online`
    (`internal/myhome/shelly/gen1/listener.go:128-131`). Whether the device ID in the listener includes the model
    prefix was not checked.
21. **Accuracy of quotations.** The web quotations in this file came through a fetch tool that summarises the page.
    Where a phrase is in quotation marks, it was extracted by that tool and was not checked character by character
    against the page. Re-check the wording before quoting it externally. The pricing-change sentence (D.2) and the
    Google 7126229 sentence (C.3) are the two most important to re-check.
22. **Dated sources.** Fetched 2026-11-01. Pages without a version or date are marked as such. The Go pkg.go.dev
    page shows `go1.27.3`.

### Related, not asked (short)

- `myhome/devices/impl/manager.go:663-664`: when `GetAllDevices` fails, `runDeviceRefreshJob` logs and returns,
  so the refresh goroutine stops for the life of the process. Adjacent to alerting, not evaluated further.
- `docs/observability/research-community.md` and `docs/observability/research-storage-charts.md` exist in this
  folder, written by other work. Not read for this file.
- Shelly script heap is tight (CLAUDE.md "about 7 KB"). That constrains any device-side alert, as in F.3.

---

## Progress log

- [x] A. Local code, file:line (A.1 to A.9)
- [x] B. Rule evaluation: Prometheus, Alertmanager, Grafana (B.1 to B.6)
- [x] C. Email: net/smtp vs go-mail, Gmail settings, app passwords, limits, timeouts, retry (C.1 to C.5)
- [x] D. WhatsApp (CallMeBot, Cloud API), other free routes, shoutrrr and in-repo notifier pattern (D.1 to D.7)
- [x] E. Attachment points for evaluation and banner (file:line)
- [x] F. Watchdog and independent check, NAS-down analysis (F.1 to F.4)
- [x] G. Could not establish (items 1 to 22)

Scope note: no code written, no live system touched (no device calls, no `events.db` queries, no service
changes). Secret files were not read; `/var/lib/myhome/.env` was referenced only through the unit file's
`EnvironmentFile=` line.
