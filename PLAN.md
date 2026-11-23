# Plan: extract `pkg/shelly` as a separate module (abandoned)

**Status: abandoned on 2026-09-06.** The extraction of `pkg/shelly` into a standalone repository
(`github.com/asnowfix/go-shellies`) is not going to happen. PR #212 was closed, and the decision is
recorded in #596. The shared-boundary rule still applies: `pkg/shelly/` must not contain
MyHome-specific code (see `CLAUDE.md`, three-tier layer rule).

If a reusable Shelly library is wanted later, start from a fresh statement of why, and re-run the
split from current `main` with `git filter-repo`. Do not revive the closed branch.
