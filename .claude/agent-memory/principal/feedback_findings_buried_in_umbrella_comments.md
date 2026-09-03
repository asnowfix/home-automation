---
name: feedback-findings-buried-in-umbrella-comments
description: A finding written as "worth its own issue" inside a long umbrella comment is not tracked work — file it in the same turn or it is lost
metadata:
  type: feedback
---

On this project, campaign umbrellas (#401, and its handovers #548 → #559) accumulate very long
comments that mix results, evidence and *new findings*. A finding recorded inside one of those
comments — even when the comment explicitly says "worth its own issue" — **does not become tracked
work**, and nothing ever notices.

Observed 2026-08-31, resuming #559 cold. The 2026-08-30 gate-3 comment on #401 named two findings as
needing their own issue. Neither had one, five days later:

- the solar start hold-timer resetting on any single sub-threshold sample, which made the **shipped
  500 W default structurally unable to arm** — filed that day as #585;
- `tools/monitor-solar.sh` observing nothing for 1h40m without saying so — filed as #586.

Both were substantive and user-visible. The first plausibly explains three failed gate attempts. They
survived only because someone re-read a 100-comment thread line by line.

**Why:** a comment is addressed to whoever is reading the thread *now*. An issue is addressed to
whoever picks the work up *cold*, which is the actual audience — see the self-contained-issue rule in
`CLAUDE.md`. The umbrella is a status board, not a backlog; things written on it are read as context,
not as work.

**How to apply:**

- When a session produces a finding worth acting on, **file the issue in the same turn as the comment
  that describes it**, and reference the issue number from the comment. "Worth its own issue" written
  without an issue number is a defect in the record.
- When resuming a campaign cold, **grep the umbrella's recent comments for `worth its own issue`,
  `needs its own issue`, `should be filed`, `TODO`** before planning anything. Assume nothing was
  filed and check `gh issue list` for each hit.
- Also verify factual claims in umbrella comments against the artifacts they describe before
  propagating them. The same 08-30 comment stated monitor-solar.sh "produced zero bytes in 1h44m"; the
  log was 997 bytes across 22 rows, and the real failure was completely different. A wrong summary
  points the next investigator at the wrong subsystem — see [[feedback-verify-restore-obligations]].

## The follow-on trap: a rescued finding inherits its comment's framing

Filing the buried finding is only half the job. **The comment's characterisation of it has usually
never been checked either**, and lifting it into an issue laundered it into a premise.

2026-08-31 I filed #585 from a comment claiming the pool pump's shipped 500 W solar threshold was
structurally unable to arm. One `events.db` query the next day showed the pump starting ~11:22 on two
consecutive days, 92 minutes before its window, at the unmodified 500 W default — the opposite.
The *mechanism* in the comment was real; its *scope* was wrong, and I had carried the scope verbatim
into a title. Corrected on 2026-09-01, and the issue downgraded to "may close".

**How to apply:** when promoting a finding out of a comment, verify its scope separately from its
mechanism, and state the **observed rate** in the issue — `CLAUDE.md` requires it and it is exactly
what catches this. If the rate cannot be established from `events.db`, say "never observed" or "once,
under conditions X", and never let the title assert more than the evidence. A one-line query beats an
issue that sends someone to spend heap defending a failure that has happened once.
