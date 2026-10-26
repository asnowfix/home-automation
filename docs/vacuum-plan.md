# Robot vacuum control — implementation plan (#606)

Epic: #606. Proven reference: asnowfix/home-pi#6 (`roomba/`, Node + dorita980).
Integration decision: **pure Go** (option (a) in #606). Spike passed on 2026-10-21 against
the real robot (Roomba i7 "Edmond"). Measurements are in the #606 comments.

## Protocol facts this design relies on (all measured live, 2026-10-21)

| fact | consequence |
|---|---|
| Discovery is a UDP broadcast of `irobotmcs` to :5678. The reply JSON has `hostname: "Roomba-<BLID>"` and `ip` | Identity is the BLID; the IP is looked up, never configured |
| Discovery **can miss**: 1 of 3 runs, and 5 of 5 tries while the robot was returning to dock | Re-send the probe; fall back to the last-known IP |
| TLS: TLS 1.2, `TLS_RSA_WITH_AES_128_CBC_SHA256`, legacy renegotiation. The cert is issued by iRobot's intermediate CA, subject `CN=iRobot-<BLID>` | Skip chain verification (no root available) but **check that the CN equals the expected BLID** |
| The first connect after any previous connection is **refused**; the retry 3–5 s later succeeds (6 of 6 runs) | Retry with backoff; keep **one long-lived connection** per robot |
| MQTT 3.1.1, clientId = username = BLID, `clean=false`; the robot pushes `{"state":{"reported":{…}}}` deltas without a subscribe, **only on change** | Merge deltas into one state map; silence ≠ disconnected (keepalive decides) |
| Commands: publish `{"command":c,"time":unix,"initiator":"localApp"}` to topic `cmd`. The robot echoes it in `lastCommand` within 1–3 s | A command succeeds only when the echo with the same `time` arrives |
| Password: magic packet `f005efcc3b2900` after a HOME-button press; the password is the **longest printable-ASCII run ≥ 20** (dorita980's fixed offset is wrong on the i7). A reply of ≤ 7 bytes means "button not pressed" | `GetPassword` parses by printable run; `ErrNeedsButton` |
| The SFR box's unauthenticated `lan.getHostsList` lists the robot as `iRobot-<BLID>` with its current IP. The box has **no** API for DHCP reservations (`dhcp.*` → error 112, checked live from home-pi) | Second fallback for finding the robot's IP when UDP discovery misses; a reservation is web-UI only |
| Room clean: `start` + `pmap_id`, `user_pmapv_id`, `ordered`, `regions[{region_id,type}]`. `pmaps` in state gives `pmap_id → user_pmapv_id` | Region IDs are learned from `lastCommand` after a room clean is started from the iRobot app |

## Layout (three-tier rule)

```
pkg/roomba/                 generic local protocol: Discover, GetPassword, Client, State   (new module)
internal/myhome/vacuum.go   RPC verbs + request/response types
myhome/vacuum/              daemon service: storage (myhome.db), one client per robot,
                            region learning, RPC handlers                                    (new module)
myhome/ctl/vacuum/          CLI (RPC only, no protocol code)
internal/myhome/ui          Vacuum tab
```

## Secrets

The local password lives only in `myhome.db` (`vacuum_robots.password`) on the daemon host.
`vacuum.pair` runs the HOME-button handshake **on the daemon**, so the password never crosses
`myhome/rpc`. No RPC result, log line or CLI output carries it.

## Phases

- [x] 0. Spike: Go TLS + paho against Edmond (read state, dock, start/pause/resume/stop/dock)
- [x] 1. `pkg/roomba`: discovery, password parsing, client (retry, state merge, command ack, CN check) + tests against fake peers.
  Live check 2026-10-26 (`go test -tags live`): discovered, connected on attempt 1 with the CN check on, state read, `dock` acknowledged in 0.7 s
- [x] 2. RPC types + `myhome/vacuum` service + storage + daemon wiring
- [ ] 3. `myhome ctl vacuum` CLI
- [ ] 4. UI Vacuum tab
- [x] 5. Room learning (`vacuum_regions`) + room clean: service side, landed with phase 2 (unit-tested; live check needs region ids from the app)
- [ ] 6. Docs (`docs/vacuum.md`), live verification on Edmond via the daemon
