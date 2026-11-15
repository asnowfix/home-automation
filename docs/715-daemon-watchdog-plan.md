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
which a stopped process cannot handle, and escalates to SIGKILL after `TimeoutAbortSec`; the result is
still `watchdog`. To be checked live by the owner after merge.
