# #715 daemon watchdog: plan and progress log

Issue: #715 (systemd notify, `WatchdogSec`, `OnFailure=`, independent check). Study:
`docs/observability/observability-study.md` section 6.

Goals: a crash (SIGKILL) and a hang (SIGSTOP) each produce exactly one email; a scheduled
`RuntimeMaxSec` restart produces none; nothing sent in tests.

## Design

1. `internal/sdnotify`: `sd_notify` by hand (datagram write to `$NOTIFY_SOCKET`), no new
   dependency. No-op when `NOTIFY_SOCKET` is unset, compiles on every target (Windows included).
   Also `Heartbeat`: sends `WATCHDOG=1` every half `WATCHDOG_USEC`, but only when every registered
   health check passes.
2. `myhome/daemon`: `READY=1` at the end of startup, `STOPPING=1` at shutdown, heartbeat gated on an
   MQTT round trip (publish a nonce, wait for the echo through the broker and the client loop).
   Extra checks are appended to one slice, which is the hook for the alert evaluator (#712).
   `notify.Mailer` construction in `daemon.go` is not touched (#835).
3. `myhome daemon notify-failure <unit> [--dry-run]`: the independent sender, run by
   `myhome-failure@.service` (`OnFailure=myhome-failure@%n.service`). It is a separate process, so
   it works while the daemon is down or hung. It classifies the failure from `systemctl show`
   (scheduled `RuntimeMaxSec` restart: send nothing) and sends through the same `notify.Mailer`
   with settings read exactly as `daemon run` reads them (viper, `MYHOME_SMTP_*`, config file).
4. Packaging: unit shipped in the `.deb` (Makefile `debpkg-package`) and in
   `.github/workflows/package-release.yml`; `linux/README.md` tree.

Why a Go subcommand and not a python3 script: the SMTP settings come from viper (env, `.env`, the
YAML `smtp.host`/`smtp.port`), and reimplementing that precedence in python is a second copy that
drifts. The subcommand reuses `notify.New` and the same config keys; the `.deb` has no python
dependency. Cost: it needs the `myhome` binary to exist, which it does whenever the unit does. It
deliberately sits under `daemon`, not `ctl`: `ctl` connects to MQTT in its pre-run, which would
hang while the daemon is down.

## Progress log

### 2026-11-15 empirical: does RuntimeMaxSec + Restart=always trigger OnFailure=?

**Yes.** systemd 259 (259.9-0ubuntu0.1), user manager on the workstation, throwaway units
(removed afterwards):

```
[Unit]   OnFailure=zz715-onfail.service
[Service] Type=simple  ExecStart=/bin/sleep 1000  Restart=always  RestartSec=1  RuntimeMaxSec=4
```

Journal, repeated each cycle: `Service reached runtime time limit. Stopping.` then
`Failed with result 'timeout'.` then `Triggering OnFailure= dependencies.` then
`Scheduled restart job`. The OnFailure handler fired 3 times in 16 s.

So the 2-day restart WOULD alert unless the handler tells it apart. What the handler sees
(`systemctl show <unit>` from inside `OnFailure=name@%n.service`, unit in `activating/auto-restart`):

| failure | Result | ExecMainCode/Status | RuntimeMaxUSec |
|---|---|---|---|
| `kill -KILL` | `signal` | 2 / 9 | infinity |
| `RuntimeMaxSec=4` | `timeout` | 2 / 15 | 4s |
| `WatchdogSec=3` (Type=notify, no keepalive) | `watchdog` | 3 / 6 (SIGABRT) | infinity |

A manual `systemctl stop` does not fire OnFailure. `Result=timeout` alone is ambiguous (start/stop
timeouts also give it), so the rule is: scheduled restart iff `Result=timeout`, `RuntimeMaxUSec`
finite, and (`InactiveEnterTimestampMonotonic` - `ActiveEnterTimestampMonotonic`) >= `RuntimeMaxUSec`
(observed 4.21 s against 4 s). Anything unclassifiable alerts (fail towards telling the owner).
`RuntimeMaxUSec` is printed human-formatted (`4s`, `2d`), so the handler parses a systemd timespan.

Not covered by this experiment: SIGSTOP of the real daemon. With `WatchdogSec` systemd sends SIGABRT,
(corrected under Item 7 below: systemd also sends SIGCONT), and escalates to SIGKILL after `TimeoutAbortSec`; the result is
still `watchdog`. To be checked live by the owner after merge.

### 2026-11-15 implementation

Done: `internal/sdnotify` (+ tests, vets for GOOS=windows), `myhome/daemon/watchdog.go` (READY, STOPPING,
MQTT round-trip check, `RegisterHealthCheck` hook for #712), `myhome/daemon/notify_failure.go`
(`myhome daemon notify-failure <unit> [--dry-run]`), `linux/systemd/myhome-failure@.service`,
`myhome.service` (`Type=notify`, `WatchdogSec=60`, `TimeoutStartSec=300`, `OnFailure=`), packaging in
`Makefile` and `package-release.yml`, `linux/README.md`. No new config option (the SMTP keys already
exist), so the four-file config rule does not apply. No `go generate` change.

Noticed, not fixed (out of scope): `StartLimitIntervalSec`/`StartLimitBurst` sit in `[Service]` in
`myhome.service`, where systemd ignores them (`systemd-analyze verify` warns); they belong in `[Unit]`.
So the unit has no effective restart rate limit, which also means no `start-limit-hit` alert.

Live checks left for the owner after merge (NAS): SIGKILL, SIGSTOP, and a short scheduled restart,
each expecting exactly one email (none for the scheduled one).

## Review round 1 (PR #841): progress

### Blocker 1: stale ActiveEnter (fcc33bc5)
Test written first (start-timeout loop after a scheduled restart): it failed, then passed once
`isScheduledRestart` also requires `ActiveEnterTimestampMonotonic > InactiveExitTimestampMonotonic`
(this run reached active). Classifier cases added for `exit-code`, `start-limit-hit`, `core-dump`
(alert by default). Item 5: with `smtp.from` empty `notify-failure` now returns an error from `Send`
(after classification, so a scheduled restart still exits 0). Item 8: gofmt of `daemon.go`.

### Blocker 2: probe tolerance (1750f131)
Read `myhome/mqtt/client.go`: every `MQTT_RECONNECT_INTERVAL` (2 h) the watchdog goroutine
disconnects, reconnects and re-subscribes each topic sequentially, each wait bounded by
`MqttTimeout` (14 s default). Probes inside that window fail. Also `Publish` waits on its own
timeout, not the caller's ctx.

Changes: pet every `WatchdogSec/6` (10 s); withhold only from the 3rd consecutive failed beat
(`failureBudget`); `runCheck` abandons a check that ignores its ctx; probe publishes at QoS 1.
Tests: a single failed first probe still pets; permanently failing checks pet exactly twice; a hung
check never pets; budget table.

**Real stall tolerance (WatchdogSec=60):** a single failed probe never misses a pet (gap stays about
10 s). With the probe failing permanently: pets continue at failed beats 1 and 2 (about t0+10 and
t0+20 after the last good probe), stop at beat 3; systemd kills `WatchdogSec` after the last pet, so
about 80 to 85 s after the last good probe. The earlier statement "a stall longer than about 60 s"
was wrong.

### Item 3: postinst (5c082f13)
Chose `systemctl restart --no-block` over handling failure explicitly: postinst already polls
`ctl list` for up to 90 s and tolerates a missing daemon (script update is skipped with a message), so
it never needed the restart to be synchronous; `--no-block` removes the READY wait and the `set -e`
abort without new error paths. Timers keep the plain restart. `postinst_test.py` passes.

### Item 4: #842 (4ef04ceb, `Fixes #842`)
`StartLimitIntervalSec=300`/`StartLimitBurst=5` moved to `[Unit]` (values kept).
`systemd-analyze verify linux/systemd/myhome.service`: the "Unknown key" warning is gone (only
"/usr/bin/myhome not executable" remains, expected on a workstation).
Throwaway user unit (`ExecStart=/bin/false`, `Restart=always`, `RestartSec=1`, same limits in `[Unit]`,
`OnFailure=` marker): it stopped after the burst, `NRestarts=5`, `ActiveState=failed`. **The marker fired
5 times (about 5 s), once per crash, and no sixth time for the limit itself.** So the issue's expectation
"one email on start-limit-hit" does not hold: a crash loop is bounded at 5 emails per 300 s, not 1. Unit
comment corrected accordingly. Units removed, `reset-failed` done.

### Item 7: SIGSTOP
Review correction accepted: systemd sends SIGCONT with the watchdog abort signal, so a stopped
process most likely resumes and aborts at once (Go prints a goroutine dump); the earlier "cannot
handle SIGABRT" claim is withdrawn. Result stays `watchdog`; still to be confirmed live.
