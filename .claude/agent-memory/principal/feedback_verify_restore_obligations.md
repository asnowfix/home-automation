---
name: feedback-verify-restore-obligations
description: Re-read the device to confirm a restore obligation was discharged; never trust the comment that asserts it
metadata:
  type: feedback
---

Live experiments on `filtration-hiver` (the production pool pump) routinely disable something and
incur a **restore obligation** — re-enable schedule job 3 (`handleMorningStart`), restore a threshold,
restore the run window. The session that does this writes "restored, verified" in its own comment.

**Treat that sentence as a claim, not as state.** On resuming, re-read the device.

Observed 2026-08-31: the gate-3 experiment of 08-30 disabled schedule job 3 and the comment asserted
it was "re-enabled 11:59:22, verified". A fresh `Schedule.List` confirmed it *was* in fact enabled —
but the check cost one read-only RPC, and the failure it guards against is severe and silent: with
job 3 left disabled the pump only runs when solar is strong, so on the next cloudy day the pool
silently does not filter and nobody notices until the water does.

**Why:** an assertion written by a session that then ended is indistinguishable, to the next reader,
from an assertion by a session that was interrupted between deciding to restore and doing it. The
cost of checking is one RPC; the cost of not checking is unbounded and invisible.

**How to apply:** on every resume that follows a live experiment, before planning anything, read back
the specific facts the experiment mutated — `Schedule.List` for job enable flags and the window,
`Script.GetStatus` for `running` and a settled `mem_free`, `Switch.GetStatus` for the relay — and
record the readings with a timestamp as *measured this session*. Note in the update that they were
re-read rather than quoted. Related: [[feedback-findings-buried-in-umbrella-comments]].
