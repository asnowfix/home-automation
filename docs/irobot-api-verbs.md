# iRobot API verbs: what exists, what we use, what we verified

Catalog for design discussions (room names: local DB vs cloud as source of truth, #628). It lists every
call known for the Roomba **i7** ("Edmond", firmware 22.52.10, SKU i715640, FR), whether we use it, and
whether we verified it on our robot or account. Detail, sources and confidence reasoning:
[`irobot-local-api-research.md`](irobot-local-api-research.md). Nothing here is iRobot documentation:
iRobot publishes none for either API, so everything outside "verified by us" is community
reverse engineering.

Status column: **VERIFIED** = observed on our robot/account (date); **USED** = our code sends it, not
yet observed live; **COMMUNITY** = documented by a third-party project only; **N/A for us** = exists for
other robot generations.

## 1. Local API (robot, MQTT over TLS, port 8883; discovery UDP 5678)

One local client at a time. Commands are JSON published on topic `cmd`; the robot confirms by echoing the
command in `lastCommand`. State is a "shadow" pushed as `{"state":{"reported":{...}}}` deltas.

| Verb | Payload (besides `command`, `time`, `initiator:"localApp"`) | Effect | Status |
|---|---|---|---|
| `start` (no args) | none | Full clean | VERIFIED 2026-10-29 08:23 (robot went to `cycle: clean`, `phase: run`) |
| `start` + regions | `pmap_id`, `user_pmapv_id`, `ordered`, `regions:[{region_id,type:"rid"}]` | Room clean | USED; **not yet observed live**. Single-room test planned |
| `stop` | none | Stop the mission | VERIFIED (2026-10-26, #606) |
| `pause` / `resume` | none | Pause / resume | VERIFIED (2026-10-26, #606) |
| `dock` | none | Send home | VERIFIED (2026-10-26, and live test) |
| `find` | none | Plays a sound | USED (constant in `pkg/roomba`); not observed |
| `train`, `evac`, `spot` | none | Mapping run / bin evacuation / spot clean | COMMUNITY (dorita980 method list); not used by us |
| `point_clean` | `geom.center`, `p2map_id`, `user_p2mapv_id`, `params` | Spot clean at a point | N/A for us (V4 robots, from RoombaSharp notes) |
| HOME-button password handshake | magic packet on 8883 within ~20 s of holding HOME | Returns the robot's local password | VERIFIED 2026-10-28 (NAS pairing) |
| UDP discovery | `irobotmcs` broadcast to :5678 | Reply with hostname `Roomba-<BLID>`, `ip` | VERIFIED |

Reported state (read, never written): `pmaps` (`[{pmap_id: user_pmapv_id}]`), `cleanMissionStatus`
(`cycle`, `phase`, `mssnM`, `missionId`, `initiator`), `lastCommand`, `batPct`, `bin`, `name`, `sku`,
`softwareVer`, `featureFlags`, `mapUploadAllowed`, `pmapLearningAllowed`, ~70 keys in all (dump
2026-10-29). **There is no room/region key locally.** The robot's `pmaps` version moved from
`260829T114918` to `261029T090306` after the 2026-10-29 full clean.

## 2. Cloud API (iRobot AWS gateway; reverse engineered, SigV4 after login)

Login chain (VERIFIED on our account 2026-10-29, three runs): discover -> Gigya login -> iRobot login ->
SigV4-signed calls (service `execute-api`).

| Method | Path | Purpose | Side effect | Status |
|---|---|---|---|---|
| GET | `disc-prod.iot.irobotapi.com/v1/app/discover?country_code=FR` | Endpoints, Gigya API key, AWS region | none | VERIFIED 200 |
| POST | `accounts.us1.gigya.com/accounts.login` (form) | Account login (email + password) | creates a login session | VERIFIED `errorCode 0` |
| POST | `{httpBase}/v1/login/account` (JSON, `assume_robot_ownership:"0"`) | Temporary AWS credentials | none | VERIFIED 200 |
| GET | `{httpBaseAuth}/v1/user/associations/robots` | Robots on the account | **reply includes the robot's local password**; we never call it from code | VERIFIED 200 (one robot: ours) |
| GET | `.../v1/<blid>/pmaps?activeDetails=1` | Maps and **rooms**: `active_pmapv_details.regions[]` `{id,name,region_type}` | none | VERIFIED 200: 2 maps; map `e8EO…` has 8 rooms, version `251027T114719` |
| GET | `.../v1/<blid>/missionhistory` | Past missions | none | COMMUNITY; not tried |
| GET | `.../v1/<blid>/pmaps/<pmap>/versions/<ver>/umf` | Map geometry | none | COMMUNITY (LOW); not tried |
| GET | `.../v1/p2maps/<pmap>/versions[/<ver>]` | V4-robot map read | none | **VERIFIED not available for our map**: HTTP 400 `AspenError.MapNotFound: No Map Found for P2Map ID` for both forms (2026-10-29, read-only) |
| POST | `.../v1/p2maps/<pmap>/versions` `{"edit_cmd":{"command":"set_room_metadata","params":{"room_id","room_metadata":{"name","type"}}},"response_type":"link"}` | **Rename a room / set its type** | **every edit creates a new map version**; unknown whether it keeps the user-confirmed version or renumbers regions | COMMUNITY (RoombaSharp, Skamiplan/roomba-dashboard, `EDIT_PROTOCOL.md`, "verified 200" by its own capture); **never called by us** |
| POST | same URL, `command` `split_room` / `merge_rooms` / `set_virtual_wall` | Geometry edits | new map version | COMMUNITY, same source; never called |
| POST | `.../v1/user/associations/robots/<blid>?app_id=...` | Associate a robot with the account | changes the account | COMMUNITY (irbt); never called |

## 3. What this means for room names (design inputs)

- **Reading** rooms from the cloud works for our robot today (`pmaps`).
- **Renaming** is only known through the `p2maps` edit API. Our `p2maps` read answered
  "map not found" for our map id, which is evidence (not proof) that this API does not serve an i7
  `pmaps` map. No rename call for `pmaps` was found in any source. So, under the owner's rule (local DB
  becomes the source of truth only if a rename API exists), the evidence so far keeps the **cloud as
  source of truth**. A different `pmaps` write endpoint could still exist; it would have to be captured
  from the official app, which cannot run here.
- Each cloud write would create a new map version, and our room clean must send the robot's own current
  `user_pmapv_id` (read at send time). Cloud and robot versions already differ (`251027T114719` vs
  `261029T090306`), so a mismatch is the normal state.
- Open until the single-room test: whether cloud region ids are accepted by the robot locally.

## 4. Our own RPC verbs (`internal/myhome/const.go`)

`vacuum.list`, `vacuum.status`, `vacuum.command`, `vacuum.clean`, `vacuum.rooms`, `vacuum.roomname`,
`vacuum.roomsync`. CLI: `myhome ctl vacuum {list,status,start,stop,pause,resume,dock,clean,rooms,rooms name,rooms sync,pair}`.

*Written 2026-10-29. Read-only requests only were made to the iRobot cloud for this catalog.*
