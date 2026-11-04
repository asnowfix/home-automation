# Plan: touch-screen kiosk UI (#681)

Umbrella issue: #681 (design constraints, child list). Hand-over and live state: #689.
This file records the **phases and their dependency order** only; the issues hold the detail and the
hand-over issue holds the state.

Integration branch: `campaign/681-kiosk-ui`. Sub-work PRs target it and are merged there; the
campaign PR toward `main` is merged by the owner only.

## Phases

1. **Reference and design** (docs only)
   - [ ] Architecture reference `docs/architecture/ARCHITECTURE.md`
   - [ ] Design: no-polling principle and retained-topic convention (#682)
   - [ ] Design: kiosk shell (#683)
2. **Principle and shell**
   - [ ] #682 rule in `AGENTS.md`, topic convention, list of polling paths with follow-ups
   - [ ] #683 left rail, sections, design tokens, bundled font, offline banner with cached data
3. **Prerequisites** (target `main`, in parallel with phases 1-2)
   - [ ] PR #511 forecast proxy brought up to date, #541 resolved
   - [ ] PR #224 room-centric refactor brought up to date (or redone, per assessment)
4. **Sections**
   - [ ] #685 vacuum section (needs #683 only)
   - [ ] #686 pool status, daemon projection to a retained topic (needs #682, #683, #511)
   - [ ] #684 room view (needs #683, #224)
   - [ ] #687 pool controls (needs #686; hardware on `development.local` only)
5. **Experiment**
   - [ ] #688 reconnect re-emit, on `development.local` only

## Rules that shape the order

- Devices are not polled; the daemon projects existing events into retained topics (#682).
- No production device is touched; `development.local` only, read-only first, before-state captured
  before any write.
- Inter font and all assets are bundled (internet-optional).
