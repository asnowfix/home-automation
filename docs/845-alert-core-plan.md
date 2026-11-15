# #845 alert contract, core evaluator and MQTT publisher: plan and progress

Reference design: body of #843 (sections Q1, Q2, Q3, Q5 `alert_state`, Q6). Owner decisions: pure MQTT,
"MQTT is reliable", no outbox, republish, retained or expiry machinery.

## Placement

New workspace module `myhome/alert` (`go work use ./myhome/alert`), as #843 prescribes ("one workspace
module, `myhome/alert`"). One package `alert` for the contract, the evaluator, the store and the publisher:
producers import only it, and #714 will add `alert/delivery` beside it, importing only `Alert`, `Decode`
and `Validate`. A separate module (not a package inside `myhome/daemon` or `internal/myhome`) keeps
producers free of daemon dependencies and respects the layering rule: no business logic in the CLI, no
MyHome-specific code in `pkg/`. `make test` iterates every `go.work` entry that has `*_test.go` files, so
the module is covered with no Makefile change. No new third-party dependency (logr, sqlx, sqlite and
`myhome/mqtt` are already used by sibling modules). The module does not import `internal/myhome`: the
instance name is a parameter (the daemon passes `myhome.InstanceName`).

## Design decisions beyond #843's text

- `since` of an episode is the instant the condition first became true (`pending_since`), so the `for`
  wait does not shift it and a restored pending row keeps its identity.
- The evaluator is driven by producers calling `Eval(rule, observations)` on their own tick; it has no
  ticker of its own. Not calling `Eval` freezes the whole rule. A key with state but absent from the
  observations counts as inactive, except during the startup grace, when it is frozen.
- `Eval` error contract: only contract violations (`ErrContract`: bad rule, key, texts) are returned.
  Invalid texts are dropped and the last valid texts are kept, so they can never wedge a transition.
  Transient publish and persist failures are logged once; the state stays put and the next `Eval`
  retries, so callers never retry.
- Persist and delete use `context.WithoutCancel`, so a shutdown right after a publish cannot open the
  crash window.
- Once the resolve hold has elapsed, the episode is closed (resolved published) even if the condition is
  active again; the active condition then starts a new episode. resolving to firing is only possible
  within the hold.
- A persist failure after a firing re-fires after restart with the SAME `since`, which delivery dedupes
  (better than #843's "newer episode"). A failed state update after a resolve publish is handled likewise
  (a duplicate resolve for the same episode).
- `alert_state` also stores summary/detail/labels so a resolve can be built for a key that is no longer
  observed after a restart (contradicts #843 Q5 "no copy of what it published"; noted on #843).
- `Observation.Frozen` pauses a key's state machine; wall-clock timers (`for`, hold) keep running.
- No daemon wiring: no producer exists yet, so constructing an evaluator there would be dead code.
  Wiring rule for the first producer: subscribe delivery before `mc.Start()`, start the producer after.

## Progress log

- Module skeleton and plan created.
- Contract, evaluator, store, publisher and tests written. Two fixes found by tests: RE2 has no backreferences (MAC regexp rewritten); an inactive observation without labels/detail must not wipe the remembered texts (`updateTexts`).
- `go test -race ./myhome/alert/...`: ok, 1.2s test / 3s wall. `make test`: rc=0, 236 s wall.
- Review round 1 (#845 comment): history rewritten (never pushed) so no real device ID, MAC or home IP is in any commit; fixtures now use `02:00:00:..` MACs, `192.0.2.x` IPs and an invented device ID.
- Round 1 item 1, 3 and 4: failing tests written first (`edge_test.go`: invalid texts wedge, resolve crash window, absent keys in grace; all three failed) then fixed in `evaluator.go`.
- Round 1 item 5: delete failures logged, `WithoutCancel`, `Eval` error contract documented. Item 6: crash test now simulates a failed state update with a SQLite trigger and asserts the same `since`; `Validate` cases assert their error class; added tests for failed resolve publish, publish-then-persist on resolve, frozen firing row, absent keys in grace. Item 7: `go.mod` tidied (indirect requires, `go.sum`, `internal/global` replace), `go.work` re-sorting reverted to the one added line. Item 8: stale "newer episode" test comment removed with the old test.
- Round 1 verification: `go test -race` on the package ok (1.3 s test, 4 s wall); `make test` rc=0 in 78 s wall, no FAIL.
