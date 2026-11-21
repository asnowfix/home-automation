# Design: homedrive status panel and alerts in myhome

Status: draft for owner review. A UI follow-up on the kiosk campaign branch `campaign/681-kiosk-ui`
(#681), and the first slice of the alerting work in the observability epic #654.

**Revision 2026-11-21.** Alerting is now a producer only (#835 as re-scoped by the contract #843).
Sections 3 to 5 are revised accordingly; the goals, "Fit", the alternatives, the consequences and
the PR plan further down still describe the earlier option A (own `Notifier`, Mailer move, reminders)
and are superseded where they conflict: that delivery work is #714, #713 and #717.

Evidence labels:
- **(verified)**: read in code on branch `docs/homedrive-panel-design`, cut from
  `campaign/681-kiosk-ui`.
- **(observed)**: read on the live NAS on 2026-11-15, read-only.
- **(inferred)**: not traced.

`main` was not read. "None of #712/#713/#714/#717 exists" is verified on this branch only: no
`alert`, `Notifier`, `callmebot`, `whatsapp` or `homedrive` identifier appears in any `.go` file.

**Wire contract.** The normative contract is `github.com/asnowfix/home-drive`,
`homedrive/docs/design/status-v2.md`, in its **proposed** state on 2026-11-15. This doc cites it as
"contract §N". Its §7, "Consumer guidance", is normative for myhome, and this design follows it.
Section 9 lists the contract changes this design needs.

## The ask

homedrive, the Google Drive sync agent on the same NAS, restart-looped for about two months and
nobody noticed (home-drive #87). Make its health visible on the kiosk, with activity side by side
for 1 h, 24 h and 7 d. Send an alert through myhome's existing notification path when it fails.
homedrive only publishes. myhome subscribes, through its embedded broker.

## Goals and non-goals

Goals
- G1. myhome keeps the last known status of every homedrive instance (`<host>/<user>`). It
  survives a myhome restart.
- G2. A kiosk fragment shows, per instance:
  - its state (online, stale, offline, never seen, auth failure, error, unsupported schema);
  - the `counts` for 1 h, 24 h and 7 d;
  - its last error.
- G3. Alerts for: offline, restart loop, auth broken, and sync failing. Each alert gets one
  notification when it fires, one when it resolves, and a daily reminder while it stays firing.
  No duplicate is sent after a myhome restart.
- G4. Alerts are sent by email through the existing `notify.Mailer`. WhatsApp (#717) plugs in
  later, with no change to the rules.

Non-goals
- **WhatsApp delivery** (#717), a **persistent email queue** (#714) and a **global UI alert
  banner** (#713). The design leaves a seam for each.
- **Device alert rules and fleet grouping**, the rest of #712. They need the device health
  collector, #706.
- **Notifications from homedrive events.** Contract §7.7 allows them as toasts, but events are
  lossy and a status every 30 s makes them unnecessary for alerting.
- **Charts beyond homedrive's own three windows.** No `metrics.db` (#705).
- **Controlling homedrive from myhome.** homedrive is publish-only by decision.
- **New RPC verbs or `ctl` commands** to list or acknowledge alerts.
- **Detecting a NAS or power outage.** Nothing on the NAS can alert while the NAS is down
  (observability study §6, owner decision).

## Quality drivers

1. **Detection that does not depend on restart cadence.** The daemon and its embedded broker
   restart every 2 days (`RuntimeMaxSec=172800`, `linux/systemd/myhome.service:28`, verified), on
   every nightly upgrade, on a watchdog panic and on a NAS reboot. Retained messages and in-memory
   state are lost each time.
   - Scenario: homedrive dies, and myhome restarts 5 min later. The offline alert fires once, at
     most 15 min after myhome's start.
   - Scenario: homedrive restart-loops. The alert fires within one publish interval of the third
     start in an hour (contract §5.2: every attempt publishes a status).
2. **No noise.**
   - myhome restarting while homedrive is healthy sends nothing.
   - A 2-minute Wi-Fi or broker blip sends nothing.
   - A firing alert is not re-sent because myhome restarted.
3. **Internet-optional (AGENTS.md rule 1).** With the WAN down, the panel still works and nothing
   blocks. A failed email is retried with backoff, and the retry never stalls the MQTT handler or
   the UI.
4. **Bounded cost on a Pi 3B+ with an SD card.**
   - At most 8 instances.
   - Payloads of at most 16 KiB (the contract expects about 1.5 KB).
   - At most one SQLite row write per instance every 5 min in steady state.

## Fit

- **The subscriber, cache and panel fit as they are.** They follow the reference recipe: the daemon
  subscribes, holds state in memory, and renders an htmx fragment from memory (ARCHITECTURE §9;
  `682-retained-state.md` §4).
  - `mc.SubscribeWithHandler` exists (`myhome/mqtt/client.go:72`).
  - Subscriptions queue until `mc.Start()` (`daemon.go:725`).
- **Alerting needs an architectural increment.** No evaluator, persisted alert state or `Notifier`
  exists.
  - The only outbound channel is `notify.Mailer.Send(ctx, subject, body)`
    (`myhome/notify/mailer.go:21-23`).
  - **That Mailer is built only inside the notice-service branch** (`daemon.go:415-445`), and that
    service is off by default (`run.go:92`). So alerts cannot reach email today unless the notice
    service is on.
  - The increment: a new `myhome/alert` module, plus building the Mailer outside the notice
    branch (§5).

**ARCHITECTURE.md is wrong or stale here.** A `principal-documenter` refresh is due.
- §2 and §14 say the daemon restarts every 6 h. Since #695 it restarts every 2 days
  (`myhome.service:28`). The brief for this design also assumed 6 h. The design does not depend on
  the cadence.
- §7.1 infers an external broker on the NAS. In fact the unit runs `myhome daemon run --ui-port 80`
  with no `--mqtt-broker` (`myhome.service:9`), so it uses the embedded Mochi broker. This agrees
  with CLAUDE.md and the observability study §4. The broker allows anonymous clients (observed).
- §5.2 and §5.6 predate `newMux` and `server_test.go` (`server.go:124`). §16.5 says "no nav", but
  the shell has a rail with Rooms, Devices, Pool, Vacuum, Events and Reload (`index.html:46-73`).
- `accounts.Status.LastOK` is a **bool**, not a timestamp (`registry.go:21`).

## Proposed design

### 1. Subscriber and cache: `myhome/homedrive` (new workspace module)

**One subscription: `homedrive/+/+/status`.** The contract publishes it with QoS 1, retained, every
`publish_interval_s` (default 30 s), and also at start, on an error exit and on `/reload`
(contract §5.3). `<host>` and `<user>` are topic levels 2 and 3.

Topics this design does not subscribe to:
- `events/#`: advisory (contract §5.3, §7.7), and `push.success` is per file, so it floods during a
  bulk sync.
- `online`/LWT: a hint only (§7.3).

**Liveness: the age of the status document's own `ts`.** This follows contract §7.1.
- A brief, earlier option was to use "any message, events included", because no status was seen
  live. It is dropped now that the reason is known: the status publisher was never wired
  (contract §2).
- Status v2 is a strictly better heartbeat, because it is also published on start and on an error
  exit.
- Receipt time is never used for a status. The client's periodic reconnect re-delivers retained
  messages (`client.go:411-481`), and `mqtt.Message` exposes no retained flag (`client.go:29-33`).
  Receipt time would therefore make a dead agent look fresh every 2 h.
- **Clock skew** (contract §7.2): if `ts > now + 120 s`, flag `clock_skew` on the instance and use
  the receipt time for liveness until the skew clears.

**Parsing.**
1. Drop payloads over 16 KiB.
2. Decode with `encoding/json` into a myhome-owned copy of the v2 fields. Unknown fields are
   ignored by default; do not use `DisallowUnknownFields`.
3. Read `schema` (a string, `"major.minor"`):
   - **Major 2:** use the document in full.
   - **Another major, or missing:** show "unsupported homedrive status schema". Raise no alert
     from its contents. Still apply liveness from `ts` if present (contract §7.8).
4. An unknown `state` value is shown raw and treated as degraded, never as healthy.
5. Invalid JSON: counted, and logged at most once per instance per hour.

**Where the struct lives.** Copy it rather than importing `home-drive/pkg/homedrive` (contract Q1).
Importing it would put the home-drive module graph, including rclone, into myhome's `go.sum`, and a
decode-only consumer gains nothing from it. Drift is caught by a fixture test against the
contract's example documents (request 1 in §9).

**Cache.** `Cache` is mutex-guarded and holds `map[instanceKey]*Instance`. Each `Instance` holds:
- `LastTs`, which is the payload `ts` or, under skew, the receipt time;
- the last parsed `Status`;
- the raw last payload;
- a parse-problem string.

`Snapshot(now) []View` returns copies with the state already classified, so the UI and the rules
share one classification.

**Memory bound.** At most 8 instances; a ninth new key is dropped and logged once. The whole cache
stays under about 64 KiB.

**Persistence: yes, for identity, last `ts` and the last payload.** Without it, an instance that is
dead at myhome's start is simply unknown, and no offline rule can fire for it. That is the
incident's failure mode. Contract §7.1's "count from consumer start" only works if the consumer
remembers the instance at all. A table in `myhome.db` (the shared handle already given to the UI,
`daemon.go:752`; idempotent `CREATE TABLE IF NOT EXISTS`):

```
homedrive_instances(host TEXT, user TEXT, last_ts INTEGER, last_status TEXT,
                    PRIMARY KEY(host, user))
```

- **When rows are written:** on the first status from an instance after start, then at most every
  5 min, then best effort on shutdown. That is at most 288 small writes per instance per day.
- **On startup:** rows are loaded, so the panel shows a dead agent's last counts, `last_error`,
  `auth` and `bisync`. That is exactly what you need for diagnosis.
- **Forgetting:** a row unseen for 30 days is deleted, with a final "no longer tracked"
  notification (Open question 2).

**Classification.** One function. Constants come from the contract.

| State | Condition |
|---|---|
| never seen | no in-memory or persisted record (panel empty state) |
| online | `age ≤ 2 × publish_interval_s` |
| stale (greyed) | `2 × publish_interval_s < age`, not yet offline |
| offline | `now − max(LastTs, processStart) > 15 min`, i.e. counted from myhome's start if nothing has arrived since (contract §7.1) |
| overlays | `state == "error"`, `auth.state ∉ {ok, unknown}`, `dry_run`, `clock_skew`, unsupported schema |

### 2. Kiosk panel

**Where.** The existing **System status** sheet (`index.html:345-369`), under Accounts, as a "Drive
sync" block. Like Accounts, homedrive is an external service, and the sheet keeps 683's rule that
nothing opens by default. A dedicated rail item is the alternative (Open question 1).

**Route and handler.**
- `GET /htmx/homedrive` is registered in `newMux`, next to `/htmx/accounts` (`server.go:185`), so it
  does not fall through to the device proxy.
- `HTMXHandler.HomedrivePanel` reads only `Source.Snapshot(now)`, a narrow interface in `ui`.
  - A nil source renders "Drive sync status not available".
  - The handler makes no RPC and no MQTT call (Rule 3).
- The template is the package constant `homedrivePanelTemplate`, parsed once at `init`.

**Refresh.**
- `hx-trigger="load, refresh, every 30s [kiosk.online && kiosk.statusOpen()]"`.
- Opening the sheet also triggers `refresh` on the container.
- `kiosk.statusOpen()` is a one-line DOM check on the sheet's `is-active` class. The vacuum filter
  already uses a DOM predicate (`index.html:135`).
- **Why poll instead of the 682 SSE invalidation:** the transitions that matter (to stale and to
  offline) happen when *no* message arrives, and the ages must be re-rendered anyway. The poll
  reads memory only, and runs only while the sheet is open and the daemon is up: 2 requests per
  minute.

**Layout.** The sheet body scrolls. Text is at least 14 px. Nothing is hover-only. No emoji.

```
Drive sync  <user>@<host>                [Online]  v0.2.0 · up 3 d 4 h   [dry run]
Status 12 s ago (14:32)          State running        Google auth ok (refreshed 13:40)
                 1 h            24 h             7 d
Uploaded         3 · 1.2 MB     140 · 2.1 GB     900 · 9.4 GB
Downloaded       0 · 0 B        12 · 30 MB       80 · 410 MB
Errors           0              2                5
Conflicts        0              0                1
Starts           1              1                2
Pending uploads 0     Last push 14:20   Last pull 14:05   Last poll 14:32
Last error   push · 13:58 · rate limit exceeded… (truncated to 120 chars)
Last bisync  13:00 · ok · 42 s · 3 files      Local 12 345 files · 81 GB      Quota 63 %
Alert        Restart loop: firing since 13:10           (only while an alert is not inactive)
```

- The state pill reuses the accounts tag classes:

  | Class | States |
  |---|---|
  | `is-success` | online |
  | `is-light` | stale, starting, paused, unsupported |
  | `is-danger` | offline, `state == error`, bad auth |

- A `null` value renders `--`.
- Ages are computed on the server.
- The 1 h windows are accurate to 5 min (contract §5.3).

**Reusing the accounts pattern.** Only the *visual* pattern is reused: the box, the tag classes,
the "last checked" line and `truncateError` (`htmx.go:46-107`). **The `accounts.Registry` type is
not reused.** `Report()` stamps `LastAttempt = now` only when an integration calls it
(`registry.go:51-63`), and the type has no notion of age. A silent homedrive would show "Connected"
forever.

### 3. Alert rules (`myhome/homedrive/rules.go`, contract §7.4–§7.6)

**Revised 2026-11-21 (producer-only scope).** Per the owner-approved contract #843, homedrive is a
**producer**: it evaluates the rules below, hands each reading to the shared core (§4), and the core
publishes alert documents. Delivery (banner, email, WhatsApp) belongs to #713, #714 and #717 and is
not part of this design.

**When rules run.** On every producer tick (`EvalEvery`, 30 s), per instance. All rules are subject
to the core's startup grace (§4).
- **The content rules (R2–R5) freeze while the instance is not online** (offline, stale, a schema
  major we do not understand, or an unparsable persisted row). They neither fire nor resolve from a
  document that may be hours old. R1 owns that case.

**What alert documents contain.** Names, times and counts only, **never `last_error.message`**, a
file name or a path (a test asserts it). `summary` is `homedrive <what>: <user>@<host>` (at most 100
chars); `detail` is a fixed template (at most 400). Times are UTC. A `<user>@<host>` that would fail
`Validate` (named like an IP or a MAC, say) falls back to `homedrive <what> on an instance`.
`labels.subject` is `homedrive:<host>/<user>` and stays local. The alert key is a readable
`[a-z0-9._-]` prefix of `<host>-<user>` (40 chars at most) plus 12 hex digits of a hash of the exact
`(host, user)`, so two instances never share a fingerprint (`a-b`/`c` vs `a`/`b-c`, `Pi` vs `pi`).

| Rule (alert `rule`) | Trigger | `for` | Resolve (held 5 min by the core) | Severity |
|---|---|---|---|---|
| R1 offline (`homedrive.offline`) | the last document is older than 15 min (§1). Judged from the document's age even right after a myhome restart; "counted from consumer start" applies only to an instance that has sent nothing yet | 0 (the 15 min is the threshold) | a fresh status | critical |
| R2 restart loop (`homedrive.restart-loop`) | `counts["1h"].starts >= 3` **or** `state == "error"` | 0 | `starts < 3` and `state != "error"` | critical |
| R3 auth broken (`homedrive.auth-broken`) | `auth.state` outside {`ok`, `unknown`, `refresh_failing`} (contract §7.5: anything not ok or unknown) | 0 | `auth.state == ok` | critical |
| R3b auth refresh failing (`homedrive.auth-refresh`) | `auth.state == refresh_failing` | 10 min | `auth.state == ok` | critical |
| R3c auth unknown (`homedrive.auth-unknown`) | an explicit `auth.state == unknown` while `state` is `running` or `starting` (an absent or null `auth` never fires, see below) | 60 min | `auth.state != unknown` | warning |
| R4 sync failing (`homedrive.sync-failing`) | failing with no success since (below) **or** (`state == running` and `now − last_poll > 15 min`; `started_at` when no poll yet) | 5 min | condition false | critical |
| R5 bisync safety net (`homedrive.bisync`) | `bisync.result == "error"`, or `bisync.at` (or `started_at` if `bisync` is null) older than 3 h; not while `state` is `paused` or `root_blocked` | 0 | result ok and age < 3 h | warning |
| R6 local root blocked (`homedrive.root-blocked`) | schema 2.1 `guards.local_root != null` or `state == "root_blocked"` | 5 min | guard clears | critical |
| R7 bisync deletes blocked (`homedrive.deletes-blocked`) | schema 2.1 `guards.bisync_deletes != null` | 0 | guard clears (released with `ctl allow-deletes`, or nothing left to veto) | critical |
| R8 schema unsupported (`homedrive.schema`) | the instance is online on a schema major myhome does not understand | 60 min | a supported schema | warning |

- **R1 and a restart of homedrive.** A normal restart or deploy of homedrive (seconds to a couple of
  minutes of silence) never reaches the 15 min threshold, so it never alerts. The core's `for` is not
  used for R1, because the classification already is the 15 min hold-off; adding a `for` would make
  it 30 min.
- **R2 is the incident's rule.** `counts` is persisted by homedrive, so it does not depend on
  myhome having seen each attempt (contract §7.4). The contract's third signal, "`started_at`
  changed 3 times in 30 min as seen by the consumer", is not implemented: it needs in-memory history
  that myhome's own restarts erase, and it adds nothing to `counts` (this omission is deliberate). The 1 h window means R2 resolves
  up to 1 h after the loop stops. Three deploys within an hour also trip it; that is accepted.
- **R4 "failing with no success since" (contract §7.6)** has three parts, any of which fires it:
  - the contract's total rule, kept as the floor: `counts["1h"].errors >= 5` and `last_error.ts` after
    `last_activity`. It covers errors with no direction (a failed bisync pass, a conflict) and a 3 + 3
    split;
  - push: `errors_up >= 5`, `last_error.op` is a push op, and `last_error.ts` after `last_push`;
  - pull: `errors_down >= 5` and `last_error.ts` after the success clock of the op: `last_poll` for
    `poll`, `last_pull` for the download and delete ops. So steady push failures are not masked by pull
    successes, and steady download failures are not masked by successful Changes polls.

  `last_error` is a single value, so a direction is only judged when it is the direction of the last
  failure; this is approximate by construction. The detail names the direction that matched. The
  5 min `for` absorbs one bad poll cycle.
- **R3b uses a 10 min `for`, where the contract says "immediately".** `refresh_failing` comes from
  an `oauth2.RetrieveError`, which may be a transient Google 5xx (inferred; request 3 in §9). The
  definitive states (R3) stay immediate.
- **R3c and an absent `auth`.** status-v2 §12 says an absent `auth` is to be treated as `unknown`, but
  until home-drive#142 ships the NAS publishes `auth: null` on every document, and reading that as
  `unknown` would raise a warning an hour after every deployment. So R3c fires only on an explicit
  `unknown`; an absent or null `auth` means "not reported yet" and raises nothing. `starting` counts
  as up so a homedrive restart does not reset the 1 h clock.
- **R8 and an unsupported major.** §7.8 asks for no alert from contents myhome cannot read, so open
  content alerts of such an instance stay frozen. They would otherwise hang silently, so R8 raises one
  warning (after 1 h on the unknown major) saying that the instance's alerts are paused. It resolves
  when the schema is supported again, and the frozen alerts then resolve or continue normally.
- **Warning severity** (R3c, R5, R8) reaches the **banner only**, the #843 default; email for warnings
  would be a #843 question.
- **Quota** has no rule. The quota manager is not wired (contract §1), and exhaustion would show up
  as R4 anyway.
- **Known limits.**
  - **An instance myhome has never seen cannot be reported offline.** R1 judges the age of a document
    it has; with none (homedrive never published since myhome first ran, or its row is gone) there
    is nothing to judge. Detecting it needs a configured list of expected instances, which is new
    scope. The persisted cache covers the restart case once an instance has been seen.
  - **An instance with an open alert is never pruned.** The cache forgets an instance unseen for
    30 days, which would make the core resolve its alerts silently, so `Prune` keeps any instance with
    a firing or resolving alert. The consequence: a decommissioned homedrive stays offline-alerting
    until its row is deleted from `myhome.db` (`homedrive_instances`) and myhome is restarted, or
    the alert is otherwise cleared (a future ack, #843 non-goal).
  - **A daemon restart during an outage keeps the alert.** R1 reads the last document's age, not the
    process start, so the restart neither resolves nor re-fires it.

- **Guard rules R6 and R7 (schema 2.1, home-drive#144).** Both mean sync is deliberately not making
  progress and needs a human, and nothing else will say so, so both are critical. R6 has a 5 min
  `for` because a mount that is not ready yet at boot looks like `missing` and clears on its own.
  R7 has none: a held-back mass deletion is the data-loss hazard the guard exists for, and it does
  not clear until released. The texts carry the reason code and the counts only. Against a 2.0
  producer, `guards` is absent and neither rule fires.

**Message texts** (summary / first line of detail):
- R1: `homedrive offline: <user>@<host>` / `No status since 14:32 UTC (16 min).` Resolved: `homedrive is reporting again.`
- R2: `homedrive restart loop: <user>@<host>` / `4 starts in the last hour; state error. Last failing step: startup.` (the `op` only, and only if it looks like an op name).
- R3: `homedrive needs Google sign-in: <user>@<host>` / `Auth state token_invalid. Re-authorise homedrive on its host.`
- R3b: `homedrive cannot refresh its Google token: <user>@<host>` / `Auth state refresh_failing. Check the network and the Google account.`
- R3c: `homedrive Google sign-in unverified: <user>@<host>` / `Auth state still unknown while running.`
- R4: `homedrive sync failing: <user>@<host>` / `7 failures in the last hour, none succeeded since. Last failing step: push.`
- R5: `homedrive bisync not healthy: <user>@<host>` / `Last bisync at 09:00 UTC failed.`
- R6: `homedrive local root blocked: <user>@<host>` / `Sync is paused: the local root is not the synced tree (marker_missing).`
- R7: `homedrive bisync deletes blocked: <user>@<host>` / `Bisync held back 412 deletions (limit 100, threshold). Review them, then release with allow-deletes.`

### 4. The shared alert core: `myhome/alert` (#845, PR #846)

The state machine is **not** written here. It is the shared core of #845, which this producer
configures and feeds; this section records only how homedrive uses it.

- **Interfaces used.** `alert.Rule{Name, Source, Severity, For, Hold}` per rule;
  `alert.Observation{Key, Active, Frozen, Summary, Detail, Labels}` per instance and tick;
  `Evaluator.Eval(ctx, rule, observations)`; `alert.NewMQTTPublisher`. homedrive's `Producer`
  builds one observation per known instance and rule, with `Frozen` set for a content rule while
  the instance is not online (§3).
- **What the core owns:** the states (inactive, pending, firing, resolving), `for`, the 5 min resolve
  hold, the 10 min startup grace, `alert_state` in `myhome.db` (so a `for` clock survives the 2-day
  restart and a restart neither re-fires an open alert nor loses its resolve), publish-then-persist,
  and `Validate` on every document. See #843 for the contract.
- **What the producer owns:** the conditions, the texts, and its own inputs: the homedrive cache
  already persists every instance in `myhome.db` (§1), as #843 asks of a producer.
- **No channel in this producer.** It publishes on `<instance>/alert/<rule>/<key>` (QoS 1, not
  retained) and knows nothing about email, the banner or WhatsApp. **Reminders, send retries with
  backoff, resolve-only-if-delivered, the `events.db` trace and the banner's open set are delivery's
  (#714, #713)**, which subscribes to those topics. The panel's "Alert" line can read
  `Evaluator.Snapshot()`.
- **Without delivery**, the documents go to a broker topic nobody subscribes to: nothing is sent.
  That is the state until #714 lands.

### 5. Daemon wiring (`myhome/daemon/daemon.go`)

- The homedrive cache and its subscription are created before `mc.Start()` (§1).
- The alert store, evaluator and producer are built before the cache's `Run` starts (the cache's
  prune must know which instances have an open alert). The producer **runs** only after `mc.Start()`
  (the contract's wiring rule: delivery subscribes first, producers start after), and the daemon waits
  for it at shutdown like for the cache, so a tick never persists into a closed storage.
- A failure to open the alert state disables homedrive alerts without stopping the daemon, and is
  reported in Accounts as `homedrive-alerts` (like `mqtt`, `beem` and `sfr`), not only logged.
- `ui.Start` gains one parameter, the homedrive source (nil allowed), which it passes to
  `NewHTMXHandler` (§2).
- **Moved to #714 (delivery):** moving `notify.Mailer` out of the notice branch, the SMTP
  configuration the email channel needs, and the Accounts entry. This producer needs no SMTP key.
- **No new config options.** Thresholds are constants, as in 682 §5, so the 4-file rule is not
  triggered.

## Alternatives considered (scope: the key decision)

| Option | What | Rough cost | Verdict |
|---|---|---|---|
| **A** | A minimal evaluator now, shaped as #712's core (states, persistence, `for`, reminders, grace, `Notifier`), with homedrive as its first rule set. Email through the existing Mailer; WhatsApp through #717 later. | 2 PRs, about 600 to 800 lines including tests. Most of the review is in PR c. | **Recommended** |
| B | Panel only now. Alerts wait for #712. | 1 PR | Rejected. #712 is blocked on #706, which is behind #704 (a hardware capture). The incident was "nobody noticed", and a panel inside a closed sheet does not fix that. |
| C | A homedrive-only notifier: one email on the offline or loop transition, a persisted "notified" flag, a daily resend. | 1 PR, about 150 lines | Rejected. It creates a second alert mechanism that #712 must later migrate. It has no `for`, resolve or grace semantics, so restarts and blips would page (driver 2). |
| D | An external dead-man service | small | Rejected by owner decisions: no external dead-man, build on myhome, internet-optional. |

**The strongest case for B:** design the evaluator once, with device rules (fleet grouping, tiers)
in view, instead of letting one consumer shape it.

**Why A still wins:**
- #712's blocker is *device data*, not the state machine.
- Grouping is additive: a rule can emit one grouped signal.
- A builds exactly what the study already fixed in §6: states, persistence, `for`, reminders, a
  10-min grace, and a pluggable notifier.

**The strongest case for C:** it is the fastest route to an email. It loses on restart behaviour
(driver 1).

**Placement alternative: a sixth rail item, "System".** It is easier to find. It costs rail height
(there are already 6 items of at least 64 px, `myhome.css:374`) and a whole section for one block
(Open question 1).

## Consequences

- **Better:**
  - A homedrive death or restart loop pages within 15 min.
  - The cause (last error, auth, bisync) is readable on the kiosk.
  - #712 starts from tested code.
  - SMTP status shows in Accounts even with the notice service off.
- **Worse:**
  - Two new workspace modules.
  - Two new `myhome.db` tables.
  - `ui.Start` grows to 12 parameters.
  - A copied struct that must track the contract. The fixture test and the "ignore unknown fields"
    rule contain that risk.
- **Harder to change later:** the `alert_state` schema and the `Rule`/`Notifier` signatures, which
  #712, #713, #714 and #717 build on.

## Cross-cutting concerns

- **Security:** not changed. The broker accepts anonymous clients (observed; `server.go:65`), so a
  LAN client could spoof `homedrive/...` and hide or fake an alert. That is the accepted LAN trust
  model. Payloads are size-capped, only JSON-decoded, and escaped by `html/template`.
- **Privacy:** emails and WhatsApp carry no error text and no file names. The kiosk shows
  `last_error.message`, truncated to 120 chars (Open question 5).
- **Observability:** parse problems are logged at most once per hour per instance. Alert
  transitions go to `events.db`. SMTP outcomes go to Accounts.
- **Failure modes:**
  - SMTP or internet down: retried with backoff, and the panel is unaffected.
  - SQLite error: logged, and in-memory operation continues. The risk is one duplicate
    notification after a restart, which is accepted.
  - Events service off: no event trace, but alerts are still sent.
  - Device manager disabled: no UI and no evaluator, the same gating as today (`daemon.go:283`).
- **Rollout:**
  - **Dependency: home-drive issue #87, branch `issue-141-status-v2 (home-drive #141)`, must be deployed on the NAS
    before PR c is merged.** Until then there is no status at all (contract §2). R1 would fire
    once and then stay firing, which is correct but noisy.
  - PR b can merge first. It shows "never seen" until homedrive v2 publishes.
  - After PR c is deployed: the 10-min grace, then a real failure fires once.
  - Each PR reverts on its own. The tables are additive.

## Impact on existing conventions

- **Extends the 682 relay pattern** with a justified exception: a gated 30 s poll, for a fragment
  whose state changes with time, not with messages.
- **Applies AGENTS.md Rule 3, "carry their own age", to an external producer.** The payload `ts`
  is RFC3339 UTC, where myhome itself uses Unix `ts`. The parser accepts RFC3339; the convention is
  not changed.
- **The Mailer moves** out of the notice service into daemon-level wiring.
- **Two `myhome.db` tables** are owned by non-device services. Precedent: `temperature_*`,
  `vacuum_*`.

## Implementation PRs (against `campaign/681-kiosk-ui`)

**Must not change in either PR:**
- the `notify.Mailer` interface;
- the notice digest's behaviour and schedule;
- the `accounts.Registry` API;
- existing fragment URLs, SSE event names and RPC verbs;
- the MQTT client.

**Test rules for both PRs:**
- No real SMTP, CallMeBot or broker in tests. Use fakes behind `Notifier` and `Mailer`.
- Call the MQTT handler method directly.
- Use in-memory SQLite (driver `sqlite`).
- Inject the clock.

**PR b: subscriber, cache and panel.**

Files:
- `myhome/homedrive/{go.mod,status.go,cache.go,store.go}` plus tests;
- `go.work`;
- `internal/myhome/ui/htmx_homedrive.go` plus `_test.go`;
- `server.go` (the route);
- `index.html` (the block in the sheet);
- `myhome.js` (`statusOpen`);
- `myhome.css` (the windows table);
- `daemon.go` (subscribe and pass the source).

Tests:
- Fixture: the contract's example documents parse.
- Parsing:
  - unknown fields are ignored;
  - `schema` `"2.7"` is accepted;
  - `"3.0"` or missing gives "unsupported", with liveness only;
  - an unknown `state` shows as degraded;
  - `null` fields render `--`;
  - a payload over 16 KiB and bad JSON are rejected.
- Classification:
  - the boundaries at `2 × publish_interval_s` and 15 min;
  - offline counted from process start;
  - the future `ts` skew case.
- A retained old document does not refresh liveness.
- The 8-instance cap.
- Persistence:
  - a round-trip;
  - a restart simulation, where a new cache on the same DB shows the last error and age;
  - the 5-min write throttle.
- Templates: every state, and no `title=`-only information.
- `httptest`: `/htmx/homedrive` returns 200 and is not proxied; a nil source renders the fallback.

**PR c: evaluator and rules.**

Files:
- `myhome/alert/{go.mod,evaluator.go,store.go,notifier.go}` plus tests;
- `myhome/homedrive/rules.go` plus a test;
- `daemon.go` (the Mailer move, starting the evaluator, the events trace);
- `htmx_homedrive.go` (the "Alert" line).

Tests:
- Transitions, table-driven.
- `for` and the resolve hold.
- Flapping sends no second notification.
- The 24 h reminder.
- Grace: a notification is held, then sent; one whose condition clears during the grace is
  dropped.
- Frozen rules while offline.
- **Restart simulation:** a new evaluator on the same DB sends no duplicate firing and keeps its
  `for` clock.
- A failed send is retried with backoff. A firing that was never delivered sends nothing when it
  resolves.
- No notification contains `last_error.message` (the fixture's message contains a path).
- R1–R5 boundaries.
- The digest uses the moved Mailer, and SMTP status is reported with the notice service off.

## Confirmation

- `make test`, with the cases listed under each PR.
- Review questions:
  - Does any rule use receipt time for a status?
  - Does any notification include error text?
  - Is any row persisted more often than every 5 min?
- On the NAS, after homedrive v2 and PR c are deployed:
  - The panel shows the real instance, with `counts` changing.
  - With homedrive stopped by the owner, one email arrives after 15 min.
  - Restarting myhome while homedrive is stopped sends no second email.
  - Starting homedrive sends one "reporting again" email after the 5-min hold.
  - A loop is tested on a **separate test instance** only (home-drive `docs/live-testing.md`).

## Open questions (recommended default in bold)

1. **Placement:** **the System status sheet**, or a sixth rail item. An attention dot on the rail
   while any alert fires belongs with #713.
2. **Instance identity:** **learned from messages, persisted, and forgotten after 30 days unseen
   with a final notice**, or a configured list (the 4-file rule applies, and the list never
   forgets).
3. **Reminder while firing:** **every 24 h**, or none.
4. **Scope:** **A**, B or C, as in the table above.
5. **Error text on the wall screen:** **truncated to 120 chars**, or only the `op` and time.
6. **Email live:** please confirm `MYHOME_SMTP_*` is set in the NAS `.env`. Without it, alerts are
   only visible on the kiosk and in Events.

## 9. Requests to the homedrive contract (`status-v2.md`)

1. **Add example documents**: running, `state:"error"`, and `auth.state:"token_invalid"`. Put them
   in the contract or in `testdata/`, so myhome can pin a fixture test to them. Without them, the
   copied struct can drift silently.
2. *(Minor bump, not blocking.)* **Per-direction error attribution**, for example
   `counts.*.errors_up` and `counts.*.errors_down`, or a per-direction `last_error`. Today R4
   cannot tell steady push failures apart from pull successes, because `last_activity` covers both
   directions.
3. **Say whether a transient token-endpoint 5xx yields `refresh_failing`.** If it does, myhome
   keeps a 10 min `for` on that state, against §7.5's "immediately". If only a definitive rejection
   can produce it, myhome drops the `for`.
4. **Cap the MQTT reconnect interval at 2 min or less** (paho's `MaxReconnectInterval` defaults to
   10 min, inferred). After a broker restart, status must resume well inside myhome's 15-min
   offline threshold. §5.2 covers the initial connect, not reconnects.
5. **Confirm that `publish_interval_s` is always present and greater than 0.** myhome falls back to
   30 s if it is missing.
