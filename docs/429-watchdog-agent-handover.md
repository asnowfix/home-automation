# Agent launch & control package — issue #429 (watchdog.js heap footprint)

This file is a **ready-to-run launch package**. A coordinating agent (or human) with no memory of
the session that produced it can start the implementation agent, supervise it, and decide whether to
accept its work, using only what is written here plus issue #429.

Nothing in here needs to be re-derived. Where a number is quoted it was measured on real hardware on
2026-08-04/05; re-verify before relying on it, but do not start from scratch.

---

## 1. What you are launching

One implementation agent to reduce `internal/shelly/scripts/watchdog.js`'s runtime heap footprint,
per **issue #429**. Read `gh issue view 429` first — it is self-contained and authoritative. This
file only covers *how to run and control the agent*.

**Model**: `sonnet`. **Isolation**: its own git worktree. **Run in background**: yes.

**Base branch**: `docs/shelly-device-constraints`, NOT `main`. That branch carries the `AGENTS.md`
sections the agent depends on ("JS Heap Budget", "Measuring one script's footprint", "Testing memory
on a spare device"). If it has already merged to `main`, base on `main` instead and say so.

---

## 2. Preconditions to check before launching

| Check | How | Expected |
|---|---|---|
| Issue exists | `gh issue view 429` | open, self-contained |
| Knowledge is in the tree | `grep -c "Measuring one script's footprint" AGENTS.md` | `1` |
| Spare device reachable | `myhome ctl shelly call -B tcp://192.168.1.2:1883 -T 60s development Shelly.GetDeviceInfo` | `shellyplus1-08b61fd98f44`, fw `1.7.5` |
| Baseline tests green | `make test` | 47 packages ok, 0 failures |

If the spare device is unreachable the agent can still do the code work, but **cannot** produce the
before/after measurement that is the entire point of #429. Do not accept the work without it.

---

## 3. The agent prompt — paste verbatim

> You are implementing GitHub issue #429 in the `asnowfix/home-automation` repo, in your own
> isolated git worktree. Branch off **`docs/shelly-device-constraints`** (not `main`) — that branch
> carries the `AGENTS.md` guidance you need. Name your branch `perf/429-watchdog-footprint`.
>
> **Read first, in this order:**
> 1. `gh issue view 429` — full context, measurements, method, and test plan. Authoritative.
> 2. `AGENTS.md` sections "JS Heap Budget", "Measuring one script's footprint (differential
>    method)", and "Testing memory on a spare device" — these encode hard-won facts; follow them
>    rather than improvising a measurement method.
> 3. `CLAUDE.md` "Shelly JavaScript" — ES5/Espruino constraints that crash devices if violated.
> 4. `internal/shelly/scripts/watchdog.js` — the script under test (236 lines).
> 5. `internal/shelly/scripts/pool-pump.js` — read `CALL_SLOTS` and `processTaskQueue` for the
>    allocate-once pattern proven to work. **Do not modify this file** — it is owned by PR #426.
>
> **Goal**: reduce `watchdog.js`'s steady-state heap footprint (measured ~2156 bytes) while
> preserving its behaviour exactly. This is NOT a minification task — top-level mangling saves only
> ~145 bytes here, and the runtime footprint is ~2.5x the minified code size. The win is in runtime
> state and allocation.
>
> **Method** (see #429 for detail): hunt per-call and per-tick allocation first (closures created
> per RPC or per timer tick, object literals rebuilt per call, string concatenation in hot paths);
> prefer fixed allocate-once structures; reduce retained state such as nested config objects and
> duplicated values. Keep the KVS-configurable surface intact.
>
> **Measure on real hardware.** Use the differential method from `AGENTS.md` on the spare device
> `development` (`shellyplus1-08b61fd98f44`): read `mem_free`, stop the script, read again. Report
> before/after. Match production conditions — same resident scripts, and replicate the real KVS
> config; a near-empty KVS gives a falsely-easy result that cost several hours during #421.
>
> **Device rules — read carefully:**
> - You MAY use `development`. Its relay drives nothing; toggling it is safe.
> - You MUST NOT touch `filtration-hiver` / `shellypro1-ec62608c0230`. That is the live pool pump.
> - You MUST NOT switch on any real pump.
> - Always restart any script you stop. `watchdog.js` is load-bearing (MQTT-failure reboots,
>   firmware updates). Leave the device exactly as you found it.
> - `script upload` takes a **bare embedded-FS name**, not a path, and frequently reports a
>   *successful* upload as a failure when a post-upload RPC times out (issue #428) — always verify
>   with `Script.GetStatus` before concluding anything failed.
> - Use `-T 60s` on device RPCs; the default 14s is too short, and devices go briefly unresponsive
>   right after `Script.Start`.
>
> **Behaviour must be preserved** — `watchdog.js` is auto-uploaded by the daemon to every device
> with no human in the loop. A regression silently breaks fleet-wide self-healing. Verify the MQTT
> watchdog still reboots after the configured consecutive failures, and the firmware check still
> schedules and runs.
>
> **Verification**: `make generate && make build` succeed; `make test` = 47 packages ok, 0 failures;
> minified output still runs in the goja emulator harness.
>
> **Hard constraints**: do NOT `git push`, do NOT create a PR, do NOT run `gh pr` anything — commit
> locally only; a push hangs on a permission prompt and will stall you indefinitely. The coordinator
> handles all pushing and merging.
>
> **Report back**: before/after footprint with the exact commands used; what you changed and why;
> anything you judged too risky to cut; how you verified behaviour is unchanged; `make test` result;
> your worktree path, branch name, commit SHAs; and a ready-to-use PR description body.
>
> Work autonomously and completely. Do not stop to ask questions — make reasonable engineering
> judgements, document them, and keep going.

---

## 4. How to control and review it

**Expect it to stall before reporting.** Every implementation agent in this campaign stopped with
"waiting for the background test run" and never produced its final report or commit. If that
happens, do not re-launch it — review its worktree directly (`git -C <worktree> status/diff`), run
the verification yourself, and commit on its behalf.

**Accept only if all of these hold:**

1. A **real before/after measurement on hardware**, not an estimate. If it only reports minified
   byte counts, it has done the wrong task — send it back to the runtime-footprint goal.
2. `make test` = **47 packages ok, 0 failures**, verified by you, not just claimed.
3. `development` left as found: `watchdog.js` (id 1) and `myhome-link.js` (id 2) both `running:
   true`. Check with `Script.List`.
4. `filtration-hiver` untouched — grep the agent's report for it; any command naming it is a
   protocol breach worth investigating.
5. Behaviour preservation is argued concretely (which code path still reboots after N failures),
   not asserted.

**Reject or push back if:** it pursues minification, weakens existing tests to make them pass,
introduces a daemon dependency (violates `AGENTS.md` "Resilience Rules"), or reports a footprint
reduction without saying how it was measured.

**A negative result is acceptable.** If the footprint cannot be meaningfully reduced without
changing behaviour, a clear "not achievable, here is the breakdown of where the ~2156 bytes go" is a
genuinely useful outcome. Say so in the prompt if you re-issue it.

---

## 5. Verification commands (for the reviewer)

```bash
W=<agent worktree>
cd $W && make generate && make test          # expect: 47 ok, 0 FAIL
GOOS=linux GOARCH=arm64 go build -o /tmp/x ./myhome   # NB: -o required, ./myhome is a directory

B="./myhome/myhome ctl shelly call -B tcp://192.168.1.2:1883 -T 60s development"
$B Script.List                                # watchdog.js + myhome-link.js must both be running
$B Script.GetStatus '{"id":1}'                # mem_used / mem_peak / mem_free
```

Differential footprint check (restart both afterwards):

```bash
$B Script.GetStatus '{"id":1}'   # baseline mem_free
$B Script.Stop      '{"id":1}'
$B Script.GetStatus '{"id":1}'   # delta = watchdog.js footprint
$B Script.Start     '{"id":1}'
```

---

## 6. Known hazards

- **The heap is shared across all scripts**; `mem_used + mem_free` is not constant (~23030 while a
  script runs, `mem_free` ~25200 with all stopped — the VM itself costs ~2 KB). Compare like-for-like
  states only.
- **`error_msg` is stale** until a newly-uploaded script actually runs. A crash trace naming a
  function the installed version does not contain is left over from the previous version.
- **UDP debug lines truncate at ~128 chars**; netcat drops datagrams on macOS even with `-k`. Use the
  Python listener in `AGENTS.md`.
- The repo **auto-merges PRs when CI goes green** — open anything gated as a **draft**.

---

## 7. State at time of writing (2026-08-05, ~01:00 CEST)

- `filtration-hiver`: v0.11.9 `pool-pump.js`, `mem_peak 17136`, pump OFF, schedules intact
  (`handleMorningStart()` 09:25, `handleEveningStop()` 16:35). **Leave alone.**
- `development`: `watchdog.js` + `myhome-link.js` running, `pool-pump.js` (id 3) stopped. Carries 24
  replicated production KVS keys and test Schedule jobs retimed to 01:00/02:00 — see PR #427's
  restore steps.
- Open: #425 (#406, gated draft), #426 (#421, draft), #427 (#422), #428, #429.
  Merged: #424 (#423 UTF-8 chunk fix).
- Branch `feat/minifier-toplevel-mangling` (esbuild + symbol map) is implemented and verified
  locally but **not yet pushed or PR'd** — separate from #429.
