# Design: homedrive status panel and alerts in myhome

Status: draft for owner review. A UI follow-up on the kiosk campaign branch `campaign/681-kiosk-ui`
(#681), and the first slice of the alerting work in the observability epic #654.

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

**When rules run.** On every evaluator tick (30 s), per instance. All rules are subject to the
startup grace (§4).
- **The content rules (R2–R5) freeze while the instance is offline or its schema is unsupported.**
  They neither fire nor resolve from a document that may be hours old. R1 owns that case.

**What notifications contain.** Names, times and counts only, **never `last_error.message`**. The
contract says it may contain a file path; a test asserts it is never included.

| Rule | Trigger | `for` | Resolve (held 5 min) | Severity |
|---|---|---|---|---|
| R1 offline | classification = offline (§1) | 0 (the 15 min is the threshold) | a status with a fresh `ts` | critical |
| R2 restart loop | `counts["1h"].starts >= 3` **or** `state == "error"` | 0 | `starts < 3` and `state != "error"` | critical |
| R3 auth broken | `auth.state` ∈ {`client_missing`, `client_misconfigured`, `token_invalid`} | 0 | `auth.state == ok` | critical |
| R3b auth refresh failing | `auth.state == refresh_failing` | 10 min | `auth.state == ok` | critical |
| R3c auth unknown | `auth.state == unknown` and `state == running` | 60 min | `auth.state != unknown` | warning |
| R4 sync failing | (`counts["1h"].errors >= 5` and `last_error.ts > last_activity`) **or** (`state == running` and `now − last_poll > 15 min`) | 5 min | condition false | critical |
| R5 bisync safety net | `bisync.result == "error"`, or `bisync.at` (or `started_at` if `bisync` is null) older than 3 h | 0 | result ok and age < 3 h | warning |

- **R2 is the incident's rule.** `counts` is persisted by homedrive, so it does not depend on
  myhome having seen each attempt (contract §7.4). For that reason the contract's third signal,
  "`started_at` changed 3 times in 30 min as seen by the consumer", is not implemented: it needs
  in-memory history that myhome's own restarts erase, and it adds nothing to `counts`. The 1 h
  window means R2 resolves up to 1 h after the loop stops. That is accepted, and the panel shows
  "running" meanwhile.
- **"Repeated" (R4)** means at least 5 final failures in the last hour, after retries (contract
  §5.1: an `Outcome.Err` is a final failure), with no success of any kind since the latest one.
  Or: the pull loop has made no successful Changes poll for 15 min. R4 uses the contract's `for`,
  plus 5 min to absorb one bad poll cycle.
  - Known weakness: `last_activity` covers both directions, so steady push failures interleaved
    with pull successes keep resetting the condition (request 2 in §9).
- **R3b uses a 10 min `for`, where the contract says "immediately".** `refresh_failing` comes from
  an `oauth2.RetrieveError`, which may be a transient Google 5xx (inferred; request 3 in §9). The
  definitive states (R3) stay immediate.
- **Quota** has no rule. The quota manager is not wired (contract §1), and exhaustion would show up
  as R4 anyway.

**Message texts** (subject / first line):
- R1: `[myhome] homedrive offline: <user>@<host>` / `No status since 14:32 (16 min). See System status on the kiosk.`. Resolved: `homedrive <user>@<host> reporting again after 2 h 10 min.`
- R2: `[myhome] homedrive restart loop: <user>@<host>` / `4 starts in the last hour; state error; last failing step: startup.` (the `op` only).
- R3/R3b: `[myhome] homedrive needs Google sign-in: <user>@<host>` / `Auth state token_invalid since 09:12. Re-authorise homedrive on the NAS.`
- R4: `[myhome] homedrive sync failing: <user>@<host>` / `7 failures in the last hour, none succeeded since 13:12. Last failing step: push.`
- R5: `[myhome] homedrive bisync not healthy: <user>@<host>` / `Last bisync 09:00: error.`

### 4. Evaluator: `myhome/alert` (new workspace module; the first slice of #712)

**Interfaces.**
- `Rule { Name() string; Evaluate(now) []Signal }`, with `Signal{Key, Active, Frozen, Severity, Summary, Detail}`.
- `Notifier { Notify(ctx, Notification) error }`, with `Notification{Rule, Key, Severity, Kind (firing|resolved|reminder|forgotten), Summary, Detail, Since}`.
- `MailNotifier` adapts a `notify.Mailer`: subject = `Summary`, body = `Detail`.
- `Multi` fans out to several notifiers. #717 adds a WhatsApp notifier that sends `Summary` only,
  because CallMeBot puts the text in a query string (study §6).

**State machine, per (rule, key).** inactive → pending (`for` running) → firing → resolving (hold)
→ inactive. These are the study's §6 states. "Resolved" is emitted as a notification on the way
back. While `Frozen`, the state does not move.

**De-duplication.**
- One firing notification per episode.
- A reminder every 24 h while firing (Open question 3).
- One resolved notification, **only if the firing notification was delivered**. A firing that was
  never delivered and then resolved sends nothing.

**Flapping.** `for` absorbs short blips. The 5-min resolve hold stops fire, resolve, fire
sequences: a condition that comes back during the hold continues the same episode.

**Persistence.** Table `alert_state` in `myhome.db`:

```
alert_state(rule, key, state, pending_since, firing_since, inactive_since,
            last_notified, delivered, next_attempt, PRIMARY KEY(rule, key))
```

- It is written only on transitions and on send attempts.
- At startup, open pending and firing episodes are restored with their timestamps, as Prometheus
  restores `for` state. A restart neither re-sends a firing notification nor resets a `for` clock.

**Startup grace: 10 min** (the study's estimate).
- The evaluator runs and transitions during the grace, but **new** firing notifications are held
  until it ends, and dropped if their condition clears first.
- Restored episodes keep their reminder schedule.
- Resolved notifications go out at once.
- In practice this matters for R2–R5. R1 cannot fire before 15 min after start anyway.

**Delivery failure.**
- The episode keeps `delivered=false` and gets a persisted `next_attempt`. Backoff starts at 1 min,
  doubles, and is capped at 30 min.
- Sends run on the evaluator goroutine, with the Mailer's 10 s timeout (`mailer.go:51-53`), never
  in the MQTT handler.
- #714 later replaces this with a real queue behind the same `Notifier`.

**Kiosk trace.** When `events.Service` runs, each firing or resolved transition is also recorded:
- `component=alert`;
- `event=alert.firing` or `alert.resolved`;
- severity `warn` or `info`;
- `device_id=homedrive:<host>/<user>`.

It then shows live in the Events section, survives the volatile journal, and gives #713 its data.
The panel's "Alert" line reads the evaluator's `Snapshot()`.

### 5. Daemon wiring (`myhome/daemon/daemon.go`)

- **Build the Mailer outside the notice branch.** Build `reportingMailer{notify.New(...)}` once,
  before the notice block (`daemon.go:421-432` moves up). Pass the same value to
  `notice.NewService` and to `alert.NewMailNotifier`.
  - `notify.New` returns the no-op Mailer when `smtp.from` is empty (`mailer.go:46-50`), so this is
    safe with no configuration.
  - `accountsRegistry.SetEnabled("smtp", …)` moves too, so SMTP status shows in Accounts with the
    notice service off.
- Create the homedrive cache and subscribe before `mc.Start()`. Start the evaluator after storage
  is open, with the homedrive rules registered.
- `ui.Start` gains one parameter, the homedrive source (nil allowed), which it passes to
  `NewHTMXHandler`.
- **Configuration the alert path needs: only the existing SMTP keys.**
  - Secrets: `MYHOME_SMTP_FROM`, `MYHOME_SMTP_TO`, `MYHOME_SMTP_USERNAME` and
    `MYHOME_SMTP_PASSWORD`, in the daemon `.env` (`EnvironmentFile`, `myhome.service:32`).
    Non-secret keys may also go under `smtp.*` in `myhome.yaml`.
  - Host and port default to Gmail STARTTLS (`run.go:96-97`).
  - Live, `myhome.yaml` has no `smtp` or `notice` keys (observed), and it is unknown whether the
    `.env` has them.
  - With `smtp.from` unset, alerts are still recorded and shown. The no-op Mailer logs "email
    disabled".
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
