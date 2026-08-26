# Field Discipline — running experiments on live hardware

Contents:
- [Standing authorizations](#standing-authorizations)
- [One experiment per device](#one-experiment-per-device)
- [Know what is installed before you probe it](#know-what-is-installed-before-you-probe-it)
- [Restart and reboot hazards](#restart-and-reboot-hazards)
- [Evidence discipline](#evidence-discipline)
- [Rollback](#rollback)
- [Capturing debug output](#capturing-debug-output)
- [Tests: the emulator is not the device](#tests-the-emulator-is-not-the-device)
- [If you are a sub-agent](#if-you-are-a-sub-agent)

These devices are not a staging environment. `filtration-hiver` filters a real pool; a script that
dies there costs a day of filtration, because every Schedule job on the device dispatches through
`script.eval` and a dead script turns all of them into silent no-ops.

---

## Standing authorizations

Granted by the maintainer, no expiry, on **`filtration-hiver` and `mezzanine` only** — without
asking:

1. Upload any script build, minified or not. **State explicitly when the two devices are not running
   byte-identical code.**
2. Write, reset or delete any KVS key under `script/pool-pump/`.
3. Restart scripts and reboot either device, any hour.
4. Switch outputs for testing:
   - `filtration-hiver`: **≥5 minutes between transitions**, deliberate cycling only
     **09:00–18:00**, and never leave the output ON while water-supply protection is active.
   - `mezzanine`: unrestricted.
5. Revert any experimental setting and restore last known-good, any hour, if a script crashes twice
   or the pump is left unsafe.

**Not authorized:** any other device; network, MQTT or firmware configuration; unmasking
`myhome-update.timer`; the pool's physical safety interlocks.

Anything not on this list needs the maintainer's go-ahead. Ask before acting, not after.

`myhome-update.timer` on the NAS is **masked deliberately** (#462) — it downgrades to
`releases/latest` and rewrites device scripts fleet-wide. Re-check `systemctl is-enabled` after any
package install.

---

## Domain fact: the pool pump's water-supply input is a LEVEL

`input:0` on `filtration-hiver` is a **water-supply level** from a gauge that refills to a target and
stops — not a fault signal, and not really an edge even though the device reports changes as events.

Normal operation therefore looks like this: the pump runs a few minutes, the level drops, the gauge
trips, `pool.pump_stop {"reason":"water supply"}` fires, and the pump resumes 20–30 minutes later
once refilled. **This is the interlock working. Do not spend a session troubleshooting it.**

Two traps it has already sprung:

- `F_WATER` is a latch that clears, so probing hours later shows `false` and looks like it never
  fired.
- `Switch.GetStatus.input.state` (instantaneous level) and `F_WATER` (last consumed edge) can
  legitimately disagree; one disagreeing sample proves nothing.

Full detail, including measured timings: `docs/pool-pump.md`, "Water Supply Input".


## One experiment per device

**Never run two things against one device.** Two workstreams on `mezzanine`, and two `make test` runs
on one machine, each produced hours of confounded results that had to be discarded entirely. The
cost is not a wrong answer — it is a *plausible* wrong answer you act on.

Before starting, establish exclusivity over every resource you will touch: the device, the local
test machine, the UDP capture port. If you cannot be sure you have it, wait.

**A result on one device does not transfer to the other.** PR #477 bought enough stack margin to
pass on `mezzanine` and still failed on `filtration-hiver`. When you run an A/B, the arms differ in
more than the variable you are testing — different resident scripts, different KVS, sometimes
different firmware. State what you controlled for.

Use a **different UDP port per device** so concurrent captures cannot interleave into one file.

---

## Know what is installed before you probe it

Assume nothing about what is running. A diagnostic written for one build, evaluated against another,
killed the production pump.

- `Script.GetStatus` → `mem_used`/`mem_peak`/`mem_free` is a reliable **build fingerprint**.
- `Script.GetCode` can be fetched back off the device and grepped for a version marker, or hashed.
- `Schedule.List` tells you whether the device has any pump schedule jobs at all. A correctly
  configured script with no schedule jobs has no run window and will do nothing, forever.
- `Script.storage` values do **not** appear in KVS. Copying a device's KVS wholesale does not carry
  `STATE.scheduleMode`; writing the `schedule-mode` KVS key has no effect on it.

---

## Restart and reboot hazards

- **A `Script.Start` on `mezzanine` restores the saved active output and switches its relay ON.**
  Never restart it casually, and never as a reflex when it looks stuck.
- Rebooting is a legitimate and often necessary step — `mem_peak` measurement requires it — but it
  changes restart conditions, and a script restarted inside its run window does **not** currently
  resume the pump (#484). Rebooting to clear a measurement is not the same as rebooting to recover.
- If MQTT RPC is dead, HTTP still works and is the recovery path. Do not assume a device is
  unreachable because one transport is.

---

## Evidence discipline

**Measured, not remembered.**

- Report what you observed **this session**, with the timestamp and the device. If you are repeating
  a figure from an issue, say so explicitly.
- Close an issue only with empirical evidence in the close comment. **Leave open anything verified
  only by inspection** — reading the code and concluding it must work is not verification.
- When you report device state, report the whole set: running state, `mem_peak`, `mem_free`, switch
  output, water-supply input, and current solar watts. A partial reading invites a wrong inference.
- Flag any crash with its **full trace**. Note that a watcher's `errors` field is useless (`["error"]`)
  — the trace is in `error_msg`.
- Say "no change" when there is no change. A status report that restates yesterday's numbers as if
  fresh is worse than silence.

---

## Rollback

You are authorized to restore last known-good at any hour if a script crashes twice or the pump is
left unsafe — and you should, rather than pressing on with a diagnosis.

Recovery over HTTP works when MQTT RPC does not. Rolling back to a known commit, minified, is a
normal and expected outcome of a failed experiment; note in the issue exactly which commit is on
which device and which known bugs that reintroduces.

---

## Capturing debug output

`myhome ctl shelly script debug <device> true` sends the script's `print()` output to a UDP listener.
Three constraints, each of which has silently destroyed a capture:

- **Lines truncate at ~128 characters.** A long `print()` is cut mid-sentence, so the actionable part
  of a diagnostic is exactly what gets lost. **Write on-device diagnostics as several short lines
  (<80 chars each), never one long one.**
- **Do not use netcat.** `nc -u -l -k <port>` silently stops recording after the first burst on
  macOS 15.7.7, `-k` notwithstanding — verified with round-trip test datagrams. Use a small
  dependency-free Python listener and prove it with a few test packets before trusting a long run:

  ```python
  import socket, sys
  s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
  s.bind(("0.0.0.0", int(sys.argv[1])))
  with open(sys.argv[2], "ab", buffering=0) as f:
      while True:
          f.write(s.recvfrom(65535)[0])
  ```

- **Logs are NUL-separated**, not newline-separated. Read them with `tr '\0' '\n' < file`.

**Leaving UDP debug enabled degrades the device and causes crashes on its own.** Enable it around a
specific experiment and disable it after — this is part of the experiment, not an afterthought.

If the dev machine has a VPN whose default route beats the LAN, `net.MainInterface()` picks the wrong
callback address and the device sends debug packets nowhere. Set it explicitly — no reboot needed
(`restart_required: false`):

```bash
myhome ctl shelly call -B tcp://<broker>:1883 -T 60s <device> \
  Sys.SetConfig '{"config":{"debug":{"udp":{"addr":"<lan-ip>:<port>"}}}}'
```

---

## Tests: the emulator is not the device

`make test` is canonical. A targeted `go test ./some/package/...` **skips the Go workspace
sub-modules** and has missed a real failure in `tools/genconfigschema`, where adding a config key
changes golden-map counts.

Tests in `internal/shelly/scripts` are **timing-flaky under CPU contention** (#393). A failure seen
while another suite is running is probably not real — re-run on a quiesced machine before believing
it, and never run two `make test` invocations on one machine.

Most importantly: the goja emulator has no stack-depth limit and no heap ceiling. **It cannot
reproduce the failures that actually kill these scripts** — the `MQTT.subscribe` stack overflow, the
`out_of_memory` at init, the 5-concurrent-RPC exhaustion. A green suite means your logic is right; it
says nothing about whether the script will start on a Pro1.

---

## If you are a sub-agent

`make test` is slow here — `internal/shelly/scripts` alone takes ~183 s, a full run several minutes —
which tempts agents into `run_in_background: true`. **Do not do this if you are a sub-agent.**
Observed in 4 of 8 agent runs (#432): the agent backgrounds the test, ends its turn to wait for the
completion notification, and never resumes. Its work is left **uncommitted** and its report,
measurements and caveats are lost — even though the tests passed.

1. **Commit your work BEFORE running the full test suite.** Then a stall costs you the report, never
   the code.
2. **Run `make test` in the foreground** with a generous timeout. Use `go test ./path/...` on a single
   package for fast iteration and save the full run for the end.
3. **Do not end your turn waiting on a background task.**

If you are the **coordinating** agent and a sub-agent reports "waiting for the background test run",
treat it as finished: review its worktree (`git -C <worktree> status` / `diff`), run the verification
yourself, and commit on its behalf. **Do not re-launch it** — the work is usually already complete
and correct.
