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
| Room clean: `start` + `pmap_id`, `user_pmapv_id`, `ordered`, `regions[{region_id,type}]`. `pmaps` in state gives `pmap_id → user_pmapv_id` | Region IDs are learned from `lastCommand` after a room clean is started from the iRobot app, or fetched from the iRobot cloud (#628) |

## Layout (three-tier rule)

```
pkg/roomba/                 generic local protocol: Discover, GetPassword, Client, State   (new module)
pkg/roomba/cloud/           read-only iRobot cloud client: discovery, Gigya + iRobot login,
                            SigV4 (standard library), Maps (#628)
internal/myhome/vacuum.go   RPC verbs + request/response types
myhome/vacuum/              daemon service: storage (myhome.db), one client per robot,
                            region learning, RPC handlers                                    (new module)
myhome/ctl/vacuum/          CLI (RPC, except `pair`, which calls pkg/roomba directly; #626)
internal/myhome/ui          Vacuum tab
```

## Secrets

Revised by #626. The robot's BLID and local password are configured like every other
integration secret: `MYHOME_VACUUM_BLID` / `MYHOME_VACUUM_PASSWORD` in the daemon's `.env`, read
through Viper, never a flag or YAML. The first build kept the password in `myhome.db`
(`vacuum_robots.password`); that column is now cleared and dropped at startup.

There is no `vacuum.pair` RPC any more. `myhome ctl vacuum pair` runs the HOME-button handshake
itself (`roomba.Pair`) and prints the `MYHOME_VACUUM_*` lines for the operator to put in `.env`;
it writes nothing. So the password never crosses `myhome/rpc`, and no RPC result or daemon log
line carries it. `pair`'s own standard output is the one place it is printed, on purpose.

## Decision: check the certificate's CN, not a pinned fingerprint

#606 §5 proposed trust-on-first-use: pin the robot certificate's fingerprint at pairing time.
The implementation checks instead that the certificate's subject is `CN=iRobot-<BLID>` for the
paired robot (`pkg/roomba/tls.go`, `verifyRobotCN`), and skips chain verification.

- iRobot's root CA is not available, so the chain cannot be verified either way.
- The CN check catches the realistic failure here: the robot's DHCP address being handed to
  another device. The robot's address has already changed once (.74 → .50).
- Edmond's certificate is valid until 2029-03-08. If a firmware update replaced it before then
  (not observed, and not ruled out), a pinned fingerprint would lock the daemon out until the robot is paired again. A CN
  check survives that.
- The trade-off: the CN check does not stop a device on the LAN that presents a self-made
  certificate with the right CN. Such a device would receive the robot's password at connect
  time. That needs an attacker on the LAN who also knows the BLID. Pinning would close this
  gap, at the cost of the lockout above, and can be added later as a stored fingerprint
  checked alongside the CN.

## Phases

- [x] 0. Spike: Go TLS + paho against Edmond (read state, dock, start/pause/resume/stop/dock)
- [x] 1. `pkg/roomba`: discovery, password parsing, client (retry, state merge, command ack, CN check) + tests against fake peers.
  Live check 2026-10-26 (`go test -tags live`): discovered, connected on attempt 1 with the CN check on, state read, `dock` acknowledged in 0.7 s
- [x] 2. RPC types + `myhome/vacuum` service + storage + daemon wiring
- [x] 3. `myhome ctl vacuum` CLI. End-to-end 2026-10-26 against a local daemon (`--instance local`, throwaway myhome.db): the daemon connected to Edmond on attempt 1; `list`/`status`/`rooms`/`dock` OK, dock acknowledged; the password value is absent from the daemon log
- [x] 4. UI Vacuum section on the main page (`/htmx/vacuum`, polled every 5 s; empty until a robot is paired). Checked 2026-10-27 over HTTP against the local daemon (browser extension unavailable): live card rendered, `dock` posted and acknowledged, an invalid command shown as an error on the card
- [x] 5. Room learning (`vacuum_regions`) + room clean: service side, landed with phase 2 (unit-tested; live check needs region ids from the app)
- [ ] 6. Docs (`docs/vacuum.md`), live verification on Edmond via the daemon
- [x] 7. Rooms from the iRobot cloud account (#628): `MYHOME_IROBOT_EMAIL/PASSWORD` in `.env`,
  `vacuum.roomsync` + `ctl vacuum rooms sync`, one background sync at daemon start (no polling).
  Unit-tested against fake servers only; the owner runs the live `rooms sync` and one single-room
  clean (results go on #606)

## Rooms from the cloud (#628)

The robot does not report rooms locally (full state dump, 2026-10-29), so their ids and names come
from the account's `GET /v1/<blid>/pmaps?activeDetails=1`. Login chain and evidence:
`docs/irobot-local-api-research.md`, "Verified on our account, 2026-10-29".

- **Names: the cloud is the source of truth** (owner decision 2026-10-29). A sync overwrites local
  names, marks rooms the cloud dropped as stale (`vacuum_regions.removed`), and `rooms name` refuses
  cloud rooms, because **no usable rename API is known for this robot** (see
  `docs/irobot-api-verbs.md`). The only known rename, `set_room_metadata` on the V4 `p2maps` API
  (RoombaSharp), does not serve our i7 map: read-only `p2maps` GETs returned HTTP 400
  `AspenError.MapNotFound`. That is "not found", not "proven absent": the official app can rename,
  and its call is undocumented. No write path exists. If a usable rename API turns up, the policy
  flips (local DB becomes the source of truth and names are synced up). The code point is
  `Service.NameRoom` / `Storage.syncCloud` in `myhome/vacuum`.
- **Versions: the mismatch is normal.** The cloud keeps the last app-confirmed map version, while the
  robot's moves on after each clean. Rooms store the cloud version for information. A clean always
  sends the robot's live version and is refused if the robot has not reported the map. A cloud room
  is "unverified", not "stale", when the versions differ.
- **Secrets.** The account password, the Gigya signature and the AWS credentials stay inside
  `pkg/roomba/cloud`, which never puts them in an error. The account's robot list carries the robot's
  local password and is not called. Tests run the real client against a fake cloud and assert
  that no secret reaches logs or RPC results.
- **Open:** whether cloud region ids are what the robot accepts locally; how long ids stay valid
  across map versions. Both are settled only by the owner's live single-room clean.
