---
name: feedback-derive-blast-radius-from-code
description: Before running a provisioning command on production, read its implementation — the name, help text and issue notes all understate what it writes
metadata:
  type: feedback
---

When offering the maintainer options for a live-device operation, **derive what the command actually
does from its source**, not from its name, its `--help`, or how a previous issue described it. State
the blast radius in the option itself, because that is what the decision is made on.

2026-09-02, verifying #528 on the production pool pump. The queued task said "re-provision
(`ctl pool setup`/`update`)". Reading the code first turned up three things nobody had recorded, each
of which would have changed the decision:

- `UpdateDevice` reconciles schedules **only `if DeviceType == "pro3"`**, and the pump is a Pro1 — so
  `pool update` was a **no-op** for the thing being verified.
- `setupDevice` rewrites **24 KVS keys**, not just schedules. I had told the maintainer the reset was
  to *schedule* state; that description was incomplete and I caught it only by reading further.
- `pool add`'s help text says "Schedules are only created on Pro3 devices" — **stale, and the opposite
  of what its own code does.**

Then a fourth appeared only at runtime: `pool add` reads the current speed from *another* pool device,
that device timed out, and the lookup **fell through to a hardcoded default**, silently changing this
pump's `speed` from `max` to `eco`. A guard written specifically to avoid a silent reset failed open.

**Why:** the context firewall applies to the maintainer too. They decide from the option text you
write, so an incomplete blast-radius description launders your own uncertainty into their approval —
and unlike a subagent, they cannot re-read the code to check you.

**How to apply:**

- Read the implementation of any provisioning/setup/reconcile command before proposing it, and list
  **every category of state it writes** in the option.
- **Capture the full before-state to a file first** — `Schedule.List`, the complete KVS map. It is what
  makes "restore" mean something. Shelly etags are content-derived, so a restored value whose etag
  matches the captured one is proof of exact restoration, not just an equivalent value.
- Diff before/after **programmatically** and report which fields are identical, rather than eyeballing.
- If reading the code changes what an already-approved option means, **say so and re-check before
  acting** — see [[feedback-findings-buried-in-umbrella-comments]] for the same failure at a different
  hop.
