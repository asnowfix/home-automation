# 775 plan: trustworthy `db export` / `db import`

Issue: #775. This plan was written from the code on `main` (f4731706), not from the issue text alone.

## Findings that change the shape of the work

1. **`main` and the issue describe different code.** The issue's "current coverage" (devices with room,
   iCal URL, vacuum robots/regions, skipped-section reporting) matches `campaign/681-kiosk-ui`, which has
   `internal/myhome/dbdump.go` (`ExportDatabase`, `ImportDatabase`, `Skipped`, `ImportCounts`). On `main`
   there is no `dbdump.go`, no vacuum code, and `myhome/ctl/db/export.go` is the older, simpler version.
   Whichever lands second will conflict on `myhome/ctl/db/*` and `dbdump.go`. Owner decision needed on order.
2. **On `main` the device export loses `room_id` entirely.** `LookupDevices` decodes the reply into
   `DeviceSummary` (manufacturer, id, mac, host, name), so room assignment, info and config are dropped.
   (The campaign fixes this by calling `device.show` per device, which is itself cache-backed.)
3. **Root cause of issue symptoms 1 and 2.** `device.match "*"` is served by `Cache.GetAllDevices`, which
   returns the in-memory slice and never reads the database. `Cache.ForgetDevice` deletes the DB row but
   leaves the device in the slice and all four index maps (#771). `device.show` also prefers the cache
   maps, so a shadowed/stale entry wins (#772). Rooms, weekday defaults and kind schedules are already
   read straight from SQLite (`rooms.Storage`), so only the **devices** section has the cache problem.
4. **Weekday defaults "0" (issue item 4): not shown to be a gap.** The section is read from table
   `room_weekday_defaults`; the CLI prints `len(map)`. A zero means the table is empty, and
   `HandleGetSchedule` then falls back to Saturday/Sunday = day-off, other days = work-day, so an instance
   that never ran `setweekdaydefault` legitimately has none. The CLI also swallows an RPC error into the
   same "0" (`if err == nil`), so 0 is ambiguous today. Which of the two happened on the owner's instance
   cannot be told from code; it needs one read of that table (or `ctl temperature` weekday listing).
   PR 3 removes the ambiguity (skipped sections are reported, not counted as 0).
5. **Import does not write `ical_url` on `main`**: `TemperatureSetParams` has no such field (only
   `room.edit` sets it), so even a perfect export would not restore it. PR 2 must use `room.edit` or extend
   the params.

## PR decomposition

| PR | Content | Est. changed lines |
|---|---|---|
| 1 | New verb `device.export`: full device records read from SQLite, bypassing the cache, sorted by id. `ctl db export` uses it for the devices section (room_id now present). Tests: stale/shadowed cache vs DB; device round trip through real SQLite. Also evict on forget (#771), only if it stays <30 lines. | ~250 |
| 2 | Import side: report created/updated/skipped, refuse a section the daemon cannot serve, do not resurrect forgotten devices, write `ical_url`; make import consistent with `room-id` KVS (issue item 2, needs decision: re-derive on import vs include in backup). Full round trip for all sections. | ~350 |
| 3 | Coverage report on stderr, non-zero exit on partial export unless `--allow-partial`, `version` field in the file. | ~150 |
| 4 | `docs/backup-and-restore.md` and format doc. | ~120 (docs) |
| later | Vacuum section: only after the vacuum code is on `main` (it is not). Rebase on the campaign merge. | n/a |
| separate | #772 (typed record shadowed by generic record): a cache-key design question, not a backup change. | not attempted |

PR 1 is the smallest useful step: it is the only change that makes the backup stop lying about devices.

## Decisions deferred to the owner

- Merge order against `campaign/681-kiosk-ui` (see finding 1). If the campaign merges first, PR 1 shrinks
  to swapping `exportDevice` onto `device.export`.
- `room-id` KVS (issue item 2): re-derive from `room_id` on import (smaller, no device I/O at import) versus
  back up device KVS (needs live device reads, violates "backup works with devices offline").
