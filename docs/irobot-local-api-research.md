# iRobot Roomba local API: room/region ids, shadow keys, and room-clean semantics (research)

**What was asked.** Gather what the community and official sources know about the iRobot Roomba
local API (MQTT over TLS :8883, UDP discovery :5678, password retrieval), focused on how to obtain
room/region ids without the iRobot app, which shadow keys carry map data on a Roomba i7 (firmware
3.x era), and what the `start` command does with and without regions. This file is a reference for
the `pkg/roomba` / `myhome/vacuum` work under epic #606. It is not a design.

**When we looked.** 2026-10-29 (sources read that day unless a date is given per claim). Community
forum pages are dated by their own post dates. GitHub sources are the `master` branch of
`koalazak/dorita980` as served on 2026-10-29. (A search surfaced both `koalazak/dorita980` and
`koalazf99/dorita980`; the `koalazf99` path returned 404 on fetch, so only `koalazak` was read.)

## 0. Our local facts (read first, not web-sourced)

Source: `docs/vacuum-plan.md` in this worktree (read 2026-10-29), and `pkg/roomba/state.go`,
`pkg/roomba/client.go` in this worktree.

- **Measured 2026-10-21 (spike), per vacuum-plan.md:** discovery is a UDP broadcast of `irobotmcs`
  to :5678; the reply has `hostname: "Roomba-<BLID>"`. TLS 1.2, `TLS_RSA_WITH_AES_128_CBC_SHA256`,
  cert subject `CN=iRobot-<BLID>`. MQTT 3.1.1, clientId = username = BLID, `clean=false`.
  First connect after a previous connection is refused; retry 3-5 s later succeeds (6 of 6 runs).
  Password: magic packet `f005efcc3b2900` after the HOME-button press; the password is the longest
  printable-ASCII run >= 20 bytes (dorita980's fixed offset is wrong on the i7). Confidence: HIGH
  for us (measured on Edmond).
- **Commands:** publish `{"command":c,"time":unix,"initiator":"localApp"}` to topic `cmd`; the robot
  echoes it in `lastCommand` within 1-3 s. Confidence: HIGH for us.
- **Room clean in our code:** `pkg/roomba/client.go` `CleanRegions` sends `pmap_id`,
  `user_pmapv_id`, `regions` (with `ordered`). `pkg/roomba/state.go` parses `pmaps` as a list of
  single-entry objects `[{"<pmap_id>":"<user_pmapv_id>"}]`, and `lastCommand` into
  `pmap_id`, `user_pmapv_id`, `regions`. Confidence: HIGH (code reading, our own).
- **Region ids are not yet known for Edmond.** vacuum-plan.md phase 5: "live check needs region ids
  from the app". So no region id has been measured on our robot yet.

Discrepancy to note up front: vacuum-plan.md says `pmaps` in state gives `pmap_id -> user_pmapv_id`.
The community sources (below) describe `pmaps` in the same way for i7-era firmware, but the HA
community also says "Maps are not given locally anymore (firmware 2+)" (see section 2). These two
statements are not contradictory on their face (they may refer to different fields or firmware),
but our measured `pmaps` should be re-checked against that claim.

## 1. Getting room/region ids and names without the app

### 1a. Local: `lastCommand` / `cleanSchedule` echo (community, MEDIUM-HIGH)

- **Method (community consensus).** Start a room clean in the iRobot app, then read the robot's
  shadow locally with `getRobotState(['lastCommand'])` (dorita980) and copy `pmap_id`,
  `user_pmapv_id`, `regions` from it. Source: dorita980 README
  <https://raw.githubusercontent.com/koalazak/dorita980/master/README.md> (read 2026-10-29): quoted
  "The easiest way to find this information is to start a clean using the iRobot app", and
  "Using this you can derive the `pmap_id`, `user_pmapv_id` and `regions` data.", and
  "Or looking into `pmaps` property in the state." Confidence HIGH that the README says this; it
  is the library's own documented workaround. (It is the same method our spike plan uses.)
- Also in HA community, bradymholt (2021-09-21): "Start a room clean in the iRobot app, then close
  the app. ... Connect to the robot's IP with `dorita980.Local` and call
  `robot.getRobotState(['lastCommand'])`." Source:
  <https://community.home-assistant.io/t/add-support-for-irobot-roomba-clean-specific-room/260650>
  (read 2026-10-29). Confidence MEDIUM (forum; consistent with dorita980 README, so HIGH on the
  method, but the thread's JSON is redacted).
- **Shape echoed in `lastCommand`.** From a 2024 HA community post by bobrippling (2024-03-24),
  quoted by the thread at
  <https://community.home-assistant.io/t/roomba-integration-remember-maps-zones-allow-user-to-tag/708279>
  (read 2026-10-29): `command`, `time`, `initiator: "rmtApp"`, `favorite_id`, `ordered`,
  `pmap_id`, and a `regions` array whose entries carry `region_id` and `params` (`noAutoPasses`,
  `twoPass`). The ids in that example were '4', '1', '5'. The `pmap_id`/`user_pmapv_id` values were
  placeholders. Confidence LOW-MEDIUM (forum example, values redacted; not verified against a
  robot).
- **`cleanSchedule2[0].cmdStr`** also carries a regions list (same thread; example regions '6','4'). Confidence LOW-MEDIUM.
- **Stability of ids from this route** (johnnyh1975, 2026-09-20, same forum thread): "`lastCommand`
  is the most reliable source" for IDs, "since a robot that has cleaned a room from the app keeps the
  IDs in its own state." This is one author's statement. Confidence LOW-MEDIUM.

### 1b. Local `pmaps` state property (community, MEDIUM)

- dorita980 README (above) names `pmaps` in the state as a source. The NickWaterton/roombapy code
  reads it: `send_region_command` does `pmaps = self.get_property('pmaps')` and `regions` is a
  property `self.get_property("regions")`. Source:
  <https://raw.githubusercontent.com/NickWaterton/Roomba980-Python/master/roomba/roomba.py> (read
  2026-10-29). Confidence MEDIUM (code reading; the code does not show the shape of `pmaps` or
  `regions` beyond use).
- **Conflict to record:** HA community thread (Roomba i7 rest980, Aug 18 2025 original post,
  <https://community.home-assistant.io/t/roomba-i7-rest980-with-selective-room-cleaning-integrate-your-irobot-roomba-with-home-assistant/921713>,
  read 2026-10-29): "Maps are not given locally anymore (firmware 2+)." Confidence LOW (single
  forum post, undated firmware, no measurement). This conflicts with the dorita980 README and the
  NickWaterton code, which both treat `pmaps`/`regions` as readable locally. Our own live `pmaps`
  read (vacuum-plan.md, `pmaps` in state gives `pmap_id -> user_pmapv_id`) supports the local
  reading for `pmaps`. Region names/rooms: see 1c.
- **Region ids in local `regions`:** the NickWaterton docstring uses `"region_id": "6"` and
  `"type": "rid"`. Confidence MEDIUM (code docstring; no values checked).

### 1c. Cloud REST: `pmaps` (community reverse engineering, MEDIUM; not verified by us)

This is the only route found that returns room **names**. Every source below is third-party
reverse engineering; none is iRobot documentation. iRobot publishes no API docs for this.

- **Login chain** (tidalf/irbt `irbt/cloud.py`, read 2026-10-29,
  <https://raw.githubusercontent.com/tidalf/irbt/master/irbt/cloud.py>, and
  <https://raw.githubusercontent.com/tidalf/irbt/master/docs/endpoints.md>):
  1. `GET https://disc-prod.iot.irobotapi.com/v1/app/discover?country_code=<CC>` — returns
     `gigya` (with `api_key`), `httpBaseAuth`, `httpBase`, `awsRegion`, `mqtt`, `irbtTopics`.
  2. Gigya `POST https://accounts.us1.gigya.com/accounts.login` (form: `ApiKey`, `loginID`,
     `password`, ...). Uses `UID`, `UIDSignature`, `signatureTimestamp` from the response.
  3. `POST {httpBase}/v1/login/account` (JSON: `assume_robot_ownership`, `signature`, `timestamp`,
     `app_id`, `uid`) returns a `credentials` object (`AccessKeyId`, `SecretKey`, `SessionToken`).
  4. Subsequent calls to `{httpBaseAuth}` are signed with AWS SigV4 (service `execute-api`,
     region `awsRegion`). A 403 triggers one re-login.
  - Note the discovery path differs from dorita980's: dorita980's
    `lib`/`bin/getPasswordCloud.js` uses `GET https://disc-prod.iot.irobotapi.com/v1/discover/endpoints?country_code=…`
    and then `POST {httpBase}/v2/login` (not `/v1/login/account`). Both are community code;
    they conflict on the exact path, and the irbt code is the one tested on an i7 (per its README).
    Record both; do not pick one without testing. Confidence MEDIUM (code reading of two projects).
  - `app_id` is hard-coded in irbt (value not recorded here). Gigya key comes from discovery.
- **Robot list:** `GET {httpBaseAuth}/v1/user/associations/robots` returns, per robot id, `name`,
  `sku`, `softwareVer`, `password`, `cap`. (`irbt` cloud.py and mock file; MEDIUM.) This means the
  cloud also returns the local password, which is a second route to the password besides the
  HOME-button handshake.
- **Maps:** `GET {httpBaseAuth}/v1/<robot_id>/pmaps` (irbt `robot.py`:
  `self._cloud.api.get(self._id, 'pmaps', params=params)` with `activeDetails: 1`).
  Room list = `maps[0]['active_pmapv_details']['regions']`, each with `id` (region id), `name`
  (room name), and `region_type` (e.g. `kitchen`, `living_room`). Also `pmap_id`,
  `user_pmapv_id` at top level. Source: <https://raw.githubusercontent.com/tidalf/irbt/master/irbt/robot.py>
  and the sample JSON in <https://raw.githubusercontent.com/tidalf/irbt/master/tests/cloud_mock.py>
  (both read 2026-10-29). **The mock is a fixture, not a captured response** — shape is MEDIUM at best.
- **Room cleaning in irbt** (`robot.py` `_make_payload`, read 2026-10-29): for `start` with room
  ids it sends `pmap_id`, `user_pmapv_id`, `regions: [{'type':'rid','region_id': <id>}]`,
  `ordered: 0`, `initiator: 'rmtApp'`, and `state: 'desired'`. (irbt's README says it has been
  "only tested with a roomba i7 and a braava M6".) This is the cloud/`rmtApp` shape. Note `ordered: 0`
  here vs `ordered: 1` in dorita980/roombapy examples. Confidence MEDIUM (code reading; tested per README
  claim only).
- **Mission history:** `GET {httpBaseAuth}/v1/<robot_id>/missionhistory` (irbt `robot.py`; mock file
  shows records with `cmd: {command, initiator, ordered, state}` and `pmaps: [{<pmap_id>: <pmapv_id>}]`).
  Confidence MEDIUM.
- **Room map geometry:** `GET .../pmaps/<pmap_id>/versions/<pmapv_id>/umf` (irbt `vector_map`). The
  mock returns `{}` (fixture, not evidence). Confidence LOW.

### 1d. Other cloud-based / third-party projects (pointers, not verified)

- **Roomba+ (HACS, johnnyh1975, 2026):** states room names come from the cloud: "Names are not
  available locally, so a cloud fetch is needed," and "the cloud map is where room names live".
  Confirms the split: ids locally (via `lastCommand`), names only in cloud. Source:
  <https://community.home-assistant.io/t/roomba-integration-remember-maps-zones-allow-user-to-tag/708279>
  and <https://github.com/johnnyh1975/ha_roomba_plus> (README and `docs/API.md` not yet read in full).
  Confidence MEDIUM (author is the integration maintainer; no code checked for the endpoint).
- **RoombaSharp (.NET)** says its "MapService" provides "room data, room areas, and cloud-side edits
  such as renaming rooms" for "newer V4 Roombas". Source: search result only
  <https://packages.nuget.org/packages/RoombaSharp> (read 2026-10-29, snippet only). Confidence LOW (not read).
- **dorita980 v2 cloud is a stub.** `lib/v2/cloud.js` throws `'Not implemented.'` unconditionally
  (read 2026-10-29, <https://raw.githubusercontent.com/koalazak/dorita980/master/lib/v2/cloud.js>).
  So the dorita980 README's "cloud" for 2.x/3.x is not implemented; only `bin/getPasswordCloud.js`
  (Gigya login → `/v2/login`) works. Confidence HIGH (code read).

### 1e. Are ids stable? Do they change on map edits?

- **Not documented by any source found.** The only statement is from the HA forum (mehalter, 2022-01-07,
  thread 260650): `user_pmapv_id` "changes to the time the map was updated" after runs; the poster says
  "the Roomba 'continues to learn'". Source:
  <https://community.home-assistant.io/t/add-support-for-irobot-roomba-clean-specific-room/260650>.
  Confidence LOW (single undated-firmware user observation).
- johnnyh1975 (2026-09-20): "`user_pmapv_id` should be included. Without it, some robots clean
  everything while the log looks successful." Confidence LOW-MEDIUM (one author).
- Our code treats the map version as changing on edit (`pkg/roomba/state.go`: "The version changes
  whenever the map is edited in the app, which makes stored region ids stale"). That is an **inference
  from the name `user_pmapv_id`**, not a sourced claim. Confidence: LOW as a fact.
- Region ids as small integers: the cloud mock uses `"id": "1"`, `"2"`; the forum examples use '1'…'27'.
  Strings of digits, small. Confidence MEDIUM (fixtures + multiple community examples).

**Verified on our robot, 2026-10-30 (one robot, one run each; HIGH for this robot, unknown in
general).** On Edmond (Roomba i7, firmware 22.52.10) the robot reported `user_pmapv_id`
`261029T090306` for map `e8EOWqiwQ6e8sGpyYU4C-w`, while the cloud's active version was `251027T114719`.
A single-room `start` for region `21` naming the robot's version was echoed, opened a mission and
aborted within ~3 s with `cleanMissionStatus.error` 224, without leaving the dock. The identical
command naming the cloud's version cleaned the kitchen (owner confirmed). Hypothesis, **unverified**:
the robot reports its latest *learned* map version, while a clean must name the version the cloud
considers active (the last one confirmed in the app). This contradicts the earlier inference in our
code that the robot's own version is the one to send.

**Cross-cutting point for question 1 (MEDIUM).** Local `lastCommand`/`regions` names ids `region_id` and
`type`; the cloud `pmaps` names them `id`, `name`, `region_type`. irbt (cloud client, tested on an i7
per its README) sends the cloud `regions[].id` as `region_id` in its start payload
(`robot.py` `_make_payload`), which implies the cloud id equals the local region id. This is
**inferred from that code**, not stated in any source. Verify before relying on it.

**Cloud password route (MEDIUM-HIGH).** Two independent code readings say the cloud returns the local
password per robot: dorita980 `bin/getPasswordCloud.js` reads `robots[BLID].password` from the
`/v2/login` response; irbt's `GET .../v1/user/associations/robots` has a `password` field (fixture
`tests/cloud_mock.py`). Both need the iRobot account login. Do not record any values.

## 2. Shadow (reported-state) keys documented for Wi-Fi Roombas

No official iRobot key list was found. Keys below are from community docs and code. The i7 is the
target. Where a source is for another model or firmware it is stated.

| key | what sources say it holds | source (read 2026-10-29) | confidence |
|---|---|---|---|
| `batPct` | battery percent | dorita980 README full-state sample; Roomba980-Python README example | HIGH (multiple) |
| `batteryType` | battery part id string | Roomba980-Python README example | MEDIUM |
| `cleanMissionStatus` | `cycle`, `phase`, `error`, `notReady`, `mssnM`, `nMssn`, `expireM`, `sqft`, `initiator` | dorita980 README sample; Roomba980-Python README; roombapy README sample | HIGH (multiple) |
| `cleanMissionStatus.cycle` values | `clean`, `quick`, `spot`, `evac`, `dock`, `train`, `none` | HA rest980 community template (forum, undated-ish 2021-2025) | LOW-MEDIUM (community template, not official) |
| `cleanMissionStatus.phase` values | `charge`, `run`, `stop`, `hmUsrDock`, `hmMidMsn`, `hmPostMsn`, `stuck`, `evac` | our `pkg/roomba/state.go` (observed on i7, per its comment); the `hm*`/`stuck`/`evac` names also in our code comments citing dorita980 docs | HIGH for `charge`/`run`/`stop`/`hmUsrDock` (measured by us); MEDIUM for the rest |
| `bin` | `present`, `full` | dorita980 README sample; Roomba980-Python README; roombapy README sample | HIGH |
| `dock` | `known` | dorita980 README sample | MEDIUM |
| `pose` | `theta`, `point{x,y}` | dorita980 README sample. Roomba980-Python: "Robots are no longer reporting tracking information" on firmware 3.20.7 | MEDIUM (see §4 for firmware caveat) |
| `lastCommand` | `command`, `time`, `initiator`, `pmap_id`, `user_pmapv_id`, `regions[]`, `ordered`, `favorite_id`, `params` | dorita980 README (`lastCommand` in sample, `command: 'dock', initiator: 'manual'`); bobrippling/HA thread 2024 (see §1a) ; vacuum-plan.md (measured echo) | HIGH for `command`/`time`/`initiator`; MEDIUM for `pmap_id`/`regions` |
| `pmaps` | list of single-entry objects `[{"<pmap_id>": "<user_pmapv_id>"}]`; the README example value is a timestamp-like `"210205T030726"` | Roomba980-Python README example; `pkg/roomba/state.go` parses the same shape (measured on Edmond per vacuum-plan.md) | HIGH (our measurement + README agree on shape) |
| `regions` | `[{"region_id": "5", "type": "rid"}]` | Roomba980-Python README example and property list; johnnyh1975 (2026-09-20) says the `type` belongs inside each region | MEDIUM |
| `cleanSchedule` / `cleanSchedule2` (`cmdStr`) | schedule; `cleanSchedule2[].cmdStr` contains a regions list | dorita980 README sample; HA thread (johnnyh1975, 2026-09-20); roombapy/Roomba980-Python code | MEDIUM |
| `wifistat` | `wifi`, `uap`, `cloud` (cloud connectivity code) | dorita980 README sample | MEDIUM |
| `signal`, `cap`, `sku`, `softwareVer`, `name`, `language`, `country`, `cloudEnv`, `tz`, `bbrun`, `bbsys`, `netinfo`, `wlcfg`, `langs` | identity, capabilities, system. `cap` has `maps`, `pmaps` counts (irbt fixture). `sku` is listed as a property in the Roomba980-Python README. Our `Status` reads `name`, `batPct`, `softwareVer` (`state.go`) | dorita980 README sample; irbt `cloud_mock.py`; Roomba980-Python README property list; our `state.go` | MEDIUM |
| `pmapLearningTimes` | **not found in any source** (two searches: no hits) | none | GAP |
| `mssnStrtTm` | **not found in any source** (search: no hits; only V4 thread hits, none naming it) | none | GAP |
| `ro-currentstate`, `ro-stats`, `rw-settings` (named shadows) | V4 (newer, e.g. Combo Essential) robots only. Not our i7 | HA forum "Reverse engineered iRobot Roomba V4 MQTT protocol" (2026-06-23 post; the thread names the shadows only in search snippets) | LOW for i7 relevance |

**Does the local shadow contain room names?** No source says so. Sources say names are cloud-only:
johnnyh1975 (HA forum, 2026-09-20): "Names are not available locally, so a cloud fetch is needed," and
the Roomba+ README says "Smart Map room names, favourites and history come from the iRobot cloud"
(<https://github.com/johnnyh1975/ha_roomba_plus>, read 2026-10-29). Also dorita980 (README): "Using this you can
derive the `pmap_id`, `user_pmapv_id` and `regions` data" — ids, not names. The rest980 forum (2025-10/11, ia74):
"many features we unfortunately cannot get from the local rest980 instance alone" (LOW-MEDIUM, third-party).
Confidence that local shadow has **ids but not names**: MEDIUM.

**Discrepancy:** the HA rest980 Roomba i7 thread (Aug 18 2025, original post) says "Maps are not given
locally anymore (firmware 2+)" (LOW). Our own measurement shows `pmaps` locally (see vacuum-plan.md); so
for the i7 `pmaps` is local even on 3.x. The forum claim may refer to map geometry (`pose`/`umf`), which
the sources do not separate.

**Cycle/phase meaning from our measurement.** `pkg/roomba/state.go` comments say the phase list is
"observed on an i7 unless noted". That is our own code comment; it is not re-checked here.

Sources for this section: dorita980 README (<https://raw.githubusercontent.com/koalazak/dorita980/master/README.md>);
Roomba980-Python README (<https://raw.githubusercontent.com/NickWaterton/Roomba980-Python/master/README.md>);
roombapy README (<https://raw.githubusercontent.com/simbaja/roombapy/master/README.md>);
HA roomba integration (<https://www.home-assistant.io/integrations/roomba/>);
HA rest980 i7 thread (<https://community.home-assistant.io/t/irobot-roomba-i7-configuration-using-rest980/161175>, template snippets via search only).

## 3. Semantics of `start`, regions, ordering, mapping and learning

### 3a. `start` with and without regions (MEDIUM-HIGH)

- **No regions = whole house.** roombapy (current `main`, read 2026-10-29,
  <https://raw.githubusercontent.com/pschmitt/roombapy/main/roombapy/roomba.py>) says in a code comment:
  "Null and empty are treated identically by the robot — the key is omitted and the robot cleans the
  whole house." Its PyPI release notes (snippet via search, <https://pypi.org/project/roombapy/>, not
  fetched successfully) say `send_command("start", {"regions": []})` raises `RoombaScopeError`, and
  "callers who want a full clean should leave out `regions` entirely." Confidence HIGH that an empty
  list means whole house according to roombapy's author; MEDIUM because the empty-list behaviour is
  stated by one library's author.
- **`pmap_id` omitted = first map.** Roomba980-Python README (REST section, read 2026-10-29):
  "if you leave `pmap_id` out, the first pmap found is used." MEDIUM (one library, REST interface).
- **`cleanRoom` = `start` with args.** dorita980 README: "`cleanRoom` is an alias for `start` - but with arguments."
  And `lib/v2/local.js` builds `{command, time, initiator:'localApp'}` then `Object.assign`s the args
  (read 2026-10-29, <https://raw.githubusercontent.com/koalazak/dorita980/master/lib/v2/local.js>). HIGH.
- **Plain `start` (no args)** in dorita980 is `_apiCall('cmd','start')` (same file). It sends no map or region.
  What the robot does with a plain `start` on a Smart-Map robot is **not documented** in these
  sources; the iRobot support page says a "Clean Everywhere" job cleans without a map (iRobot support
  article 64103 via search, see §3d). MEDIUM.

### 3b. `ordered`, `twoPass`, and other region fields

- dorita980 README: "you can set the desired order, when cleaning multiple rooms by adding `ordered = 1`";
  README also says this "works ... At least from firmware Version 3.8.3." MEDIUM (library claim; version
  given, not measured by us).
- roombapy-era Roomba980-Python code hard-codes `myCommand['ordered'] = 1` (roomba.py, read 2026-10-29).
- irbt's cloud-originated start payload uses `'ordered': 0`, `'initiator': 'rmtApp'`, `'state': 'desired'`
  (robot.py). **Conflict:** `ordered` 0 vs 1 across sources. The meaning of 0 vs 1 with a single region
  is not stated.
- `twoPass` per region: johnnyh1975 (HA forum 2026-09-20): "`twoPass` is a second vacuum pass, not
  mopping control." HA forum 2024 example shows `params: {noAutoPasses, twoPass}`. MEDIUM-LOW (forum).
- `initiator`: community examples use `localApp` (dorita980 code, our code), `rmtApp` (irbt, HA forum
  2024), and `schedule` (lastCommand examples in Roomba980-Python README). Which initiator the robot
  accepts for a local clean is not stated by any source. GAP-ish: our spike uses `localApp`, and the
  one thing we know is that it works for `dock` on Edmond (vacuum-plan.md).
- `user_pmapv_id`: johnnyh1975 (2026-09-20): "should be included. Without it, some robots clean everything
  while the log looks successful." Single author, LOW-MEDIUM. Our code always sends it.

### 3c. Mission types and cycle values

- Community cycle values (HA rest980 template): `clean`, `quick`, `spot`, `evac`, `dock`, `train`, `none`.
  LOW-MEDIUM.
- `train`: dorita980 exports a `train` method that sends `_apiCall('cmd','train')` (lib/v2/local.js,
  read 2026-10-29). The source does not describe it. Community says `train` is a mapping/"training"
  run: a forum user "describes running a few training runs, where the robot doesn't operate the vacuum or
  brushes and maps the home instead" (search snippet, not read in full). MEDIUM for "a command exists",
  LOW for its exact semantics.
- `evac`, `find`, `dock`, `pause`, `stop`, `resume`, `start` — dorita980 method list (lib/v2/local.js).
- Mapping runs in the iRobot app: "A single successful mapping-only run creates a Smart Map. The run must
  start and end on the dock and finish without errors or user help. If it fails, you must start over."
  (iRobot support article, en_GB, 64103 — which the page itself says covers i3/i4/i5; see §3d).
  The i7 has its own article: the search surfaced a general guide (article 64102) that says the robot
  "will create a second map at the end of the cleaning job" if it finds an unmapped area. The i7 guide
  was not found.

### 3d. Does a clean re-index rooms? Does a map edit change ids?

- **iRobot official (en_GB 64103, read 2026-10-29,** <https://support.irobot.co.uk/articles/en_GB/Knowledge/64103>**):**
  - "The map does not update during cleaning. Changes to the layout or moving the dock require deleting
    and remapping."
  - "Any Smart Map modification also requires deleting the map and starting a new mapping run."
  - But the same article also says: "You may edit room dividers at any time by clicking 'Room dividers'"
    (search snippet from de/co.uk/es versions) and "once rooms are defined, your robot will remember your rooms."
  - **Conflict inside iRobot's own article** (divider edits "at any time" vs "any Smart Map modification
    requires deleting the map"). Record both; do not resolve.
  - Scope: this article says it covers i3/i4/i5. Its applicability to the i7 is **not established** by it.
- **iRobot article 64102 (general guide, search snippet):** "You can use a previous Clean Map report to update
  the Smart Map ... select the three dots ... 'Update Smart Map'. You cannot undo an update." MEDIUM.
- **Community on re-learning:** mehalter (HA forum, 2022-01-07) says the robot "continues to learn," and
  `user_pmapv_id` "changes to the time the map was updated, breaking earlier scripts." LOW (one user,
  undated firmware).
- **Our `pkg/roomba/state.go` comment** says the version "changes whenever the map is edited in the app,
  which makes stored region ids stale." This is consistent with mehalter's observation about the version
  changing, but it does **not** say what triggers the change (edit vs run). The inference "edit makes region
  ids stale" is **not sourced**. Confidence in region-id stability after edits: **unknown**.
- **Does a full (whole-house) clean re-index rooms?** No source found. Nothing says a "Clean Everywhere"
  run changes region ids or learning. GAP.
- **Learning counts.** Roomba+ README (third party, 2026): "Rooms are not derived at all until three missions
  have completed" (classic 900-series cloud-derived rooms). LOW-MEDIUM; model not the i7.
- The `pmapLearningTimes` key is not in any source (GAP).

Sources for 3d: iRobot en_GB 64103 and 64102 (search results: <https://support.irobot.co.uk/articles/en_GB/Knowledge/64103>, <https://support.irobot.de/articles/en_US/Knowledge/64102>).

## 4. Local protocol details, traps, and firmware

### 4a. One-client limit (HIGH)

- dorita980 README (read 2026-10-29): "The robot only support ONE connection at time. Call `end()` to free the
  channel for the mobile app."
  <https://raw.githubusercontent.com/koalazak/dorita980/master/README.md>
- Roomba980-Python README: "As only one connection at at time is allowed to the Roomba local mqtt server"
  <https://raw.githubusercontent.com/NickWaterton/Roomba980-Python/master/README.md>
- HA core roomba integration page (read 2026-10-29, <https://www.home-assistant.io/integrations/roomba/>): the MQTT
  server "only allows a single connection." It also says continuous mode "will force the App to connect via the cloud."
- Our spike (vacuum-plan.md): "The first connect after any previous connection is refused; the retry 3-5 s later
  succeeds (6 of 6 runs)." **No community source describes this exact refused-then-succeeds pattern.** The
  closest is a dorita980 issue (mirrored on lightrun.com, seen in a search snippet only): a user with
  "connection refused on 8883" who fixed it by "removing the robot from the mobile app and setting it up again"
  (<https://lightrun.com/answers/koalazak-dorita980-connected-but-without-response>; **not read in full; LOW**). Our behaviour is therefore **measured by us, not
  documented elsewhere**. Confidence: HIGH that we measured it; UNKNOWN whether iRobot documents it.
- roombapy (`roomba.py` `watch_position` docstring): "one local connection, and two callers running
  their own loops would double the load." MEDIUM (design comment).

### 4b. TLS, ciphers, certificate

- dorita980 `lib/v2/cloud.js` is a stub; `lib/v2/local.js` (read 2026-10-29):
  `port: customOptions.port || 8883`, `ciphers: process.env.ROBOT_CIPHERS || customOptions.ciphers || 'AES128-SHA256'`,
  `rejectUnauthorized: false`. **Certificate verification is disabled.** Our CN check (`pkg/roomba/tls.go`) is
  stricter than dorita980. HIGH (code read).
- roombapy `Roomba980-Python` (roomba.py): `ssl.PROTOCOL_TLS_CLIENT`, `check_hostname = False`,
  `verify_mode = ssl.CERT_NONE`, `context.set_ciphers('DEFAULT@SECLEVEL=1')` "required for 980-era robots
  per the comments". HIGH-ish (code read; comment-based).
- HA forum J7+ (MidnightRising, 2022-06-15): add `ROBOT_CIPHERS='TLS_AES_256_GCM_SHA384'` for a Roomba J7+.
  LOW-MEDIUM (single post, other model).
- HA forum (2020-12-21, maxuser): "No cipher can be selected" (`ssl.SSLError`) was fixed by changing the cipher
  in `password.py`. LOW-MEDIUM (single post, older Python/OpenSSL).
- Our measured cipher suite `TLS_RSA_WITH_AES_128_CBC_SHA256` matches dorita980's default `AES128-SHA256` (same
  suite under OpenSSL and IANA names; this equivalence is general TLS knowledge, **not from a fetched source**).
- The "legacy" TLS flag: dorita980 applies a legacy server-connect flag "unless `ROBOT_TLS_LEGACY` is '0'"
  (read 2026-10-29, lib/v2/local.js per its summary). A roombapy issue references
  `UNSAFE_LEGACY_RENEGOTIATION_DISABLED` on newer Python (search snippet only, LOW). vacuum-plan.md lists "legacy renegotiation" as measured on Edmond.
- **No community client checks the certificate CN.** All read here use CERT_NONE / rejectUnauthorized false.
  The CN-check decision in `docs/vacuum-plan.md` is therefore **our own design**, not a community practice.
  Record as such.

### 4c. Password retrieval (magic, timing, and the 7-byte ambiguity)

- Local method, community (dorita980 README): hold HOME "until it plays tones", robot docked; "This method
  stop working for latest firmwares. If you have problems using this method please use the cloud method."
  MEDIUM (library claim; date unknown).
- HA core roomba doc (read 2026-10-29): "put the robot in pairing mode (hold Home, or Home and Spot, until it
  sounds), and submit within 30 seconds." Also: "Some models, including the Roomba j7, don't provide the password
  locally." MEDIUM-HIGH (HA is official integration docs; the 30 s window is a documented constraint, not a
  measured one).
- roombapy `getpassword.py` (read 2026-10-29,
  <https://raw.githubusercontent.com/pschmitt/roombapy/main/roombapy/getpassword.py>): sends a fixed 7-byte
  `PASSWORD_REQUEST`; if the robot answers with a separate 7-byte constant `UNSUPPORTED_MAGIC`, "the model as
  cloud-only and returns no password." Reads up to the declared length; default timeout 10 s; no retry. A robot
  "not in listening mode simply stays quiet." MEDIUM (code read).
- **Ambiguity to check against our code.** `pkg/roomba/password.go` (`ParsePassword`, read 2026-10-29) returns
  `ErrNeedsButton` for a reply of **≤ 7 bytes**, documented in its comment as "the robot's 'not now' answer, returned
  when the HOME button was not pressed." roombapy treats a 7-byte reply as a specific constant meaning
  **cloud-only model**. Community reports of "Error getting password, receive 7 bytes" (HA forum, 2021-01-06,
  Roomba 980 on software 2.4.6-3, <https://community.home-assistant.io/t/get-roomba-blid-pw-the-easy-way-using-windows/75107?page=3>)
  are the same symptom with no explanation. The two meanings are **not reconciled by any source**. We
  should not assume one; the constant bytes are the thing to compare (not recorded here).
- **Password parsing.** roombapy decodes the password "from byte 7 onward and stripped of trailing null
  bytes." Our rule (`password.go` comment, vacuum-plan.md) is the longest printable-ASCII run ≥ 20 bytes. The
  `password.go` comment says the fixed-offset slices that must not be used are "dorita980 v4 (13 or 9 bytes)".
  **Note the version:** the dorita980 material read here is the v1/v2 README and `lib/v2`; the v4 offsets were
  not read by this research. So the statement "dorita980's fixed offset is wrong on the i7" is about a
  dorita980 v4 offset that this file did not verify. **Conflict:** roombapy's rule and our rule differ. Both
  claim to parse the same reply. Not resolved here.
- **Cloud password route.** See §1 (cloud `robots[BLID].password`). Valid for any robot on the account, no HOME
  press. Trade-off: needs iRobot account credentials, sent to iRobot's auth endpoints.
- **Firmware 3.20.7 password change.** Roomba980-Python README (read 2026-10-29): "iRobot has changed the way
  passwords are retrieved in the latest firmware (3.20.7)"; the workaround given is the cloud method
  (`password.py <login> <password>`). It gives no local port, magic, or timing for the new method. MEDIUM
  (one library's README; date of the README text unknown).
- **Firmware 3.20.7 in the wild.** HA forum (2022-02-05, post on "s9 and j7", log shows
  `soho+3.20.7+soho-release-rt421+11`) — so this build exists on s9/j7 at least by Feb 2022. LOW-MEDIUM. The
  README statement's date is unknown.
- **Edmond's firmware.** Not stated in our docs. `softwareVer` is in our `Status`; the value is not in the
  repo docs. Check `softwareVer` on Edmond against 3.20.7 before drawing any conclusion.

### 4d. Firmware changes that broke or limited local access

- Roomba980-Python README (read 2026-10-29): "With the latest release of firmware (3.20.7) Robots are no longer
  reporting tracking information, therefore realtime maps will not work." MEDIUM (one library; the date of
  this statement is unknown).
- dorita980 README: "Note: some new firmwares are not reporting robot position ('pose' property) to local env."
  and "New firmware changes can cause dorita980 to stop working." and "block the robot's internet access to
  prevent OTA updates" (advice). MEDIUM.
- dorita980 README compatibility table (read 2026-10-29): firmware 1.6.x, 2.x.x (900 series uses v2), 3.2.x
  (800 series uses v3). "V4 protocol robots are work in progress." The table lists **no** simultaneous local
  clients for 2.x/3.x. MEDIUM.
- roombapy README: "This library is only for firmware 2.x.x" (PyPI/README, read 2026-10-29 via GitHub README).
  Roomba980-Python: "Firmware 2.x.x/3.x.x compatible." and "NOT Firmware 1.6.x compatible." MEDIUM (library
  claims, dated by library version, not by the firmware date).
- HA core roomba: "does NOT work with the newer x05 Wi-Fi models, such as Roomba 105, 405, and 505." MEDIUM.
  Also the V4 protocol (e.g. Roomba 105 "V4") refuses local MQTT in HA forum posts (2026 post, LOW for our i7).
- Local access on 1.6.x vs 2.x vs 3.x: the dorita980 README says local clean (`cleanRoom`/`ordered`) "works from
  firmware 3.8.3" (MEDIUM; library claim).
- Our robot runs 3.x. The i7 is in the "2.x/3.x" bucket of every source above. The 3.20.7 password change
  and tracking loss are the two firmware-dated changes found; neither is dated by an iRobot release note.
  **No iRobot release notes were found** (GAP).

### 4e. Discovery and port reachability

- Packet Mischief (Joel Knight, 2023-01-15, <https://packetmischief.ca/2023/01/15/roomba-stuck-at-verify-password/>,
  read 2026-10-29): setup uses UDP 5678 discovery then TCP 8883 MQTT; "If either step is blocked, the app stays stuck."
  Also: the app "still attempts local discovery and MQTT, then falls back to the cloud." MEDIUM (one blog,
  i3 unit).
- dorita980 troubleshooting: check connectivity with `nc -zv <robot_ip> 8883` (README, read 2026-10-29). HIGH (doc).
- dorita980 `getRobotIP`/`discovery` "need UDP broadcast enabled" (README). MEDIUM.
- Our spike: discovery missed 1 of 3 runs and 5 of 5 while returning to dock (vacuum-plan.md). **No source
  documents discovery misses.** Measured by us only.

## 5. Documented risks of guessed or wrong region ids and commands

- **No source documents what the robot does with an unknown `region_id`.** Searched; not found. GAP. (We
  should not assume it fails cleanly; johnnyh1975's "some robots clean everything while the log looks successful"
  is the only related observation, for a missing `user_pmapv_id`.)
- **Empty/null regions = whole house.** See §3a (roombapy). HIGH that the library author states this; this is
  the main silent-failure risk for a room clean.
- **Omitted `pmap_id` = first map** (Roomba980-Python REST doc). With one map this is harmless; with several
  it is a wrong-map risk.
- **Missing `user_pmapv_id`.** johnnyh1975 (2026-09-20): cleans everything while the log looks successful. LOW-MEDIUM.
- **Before dock, pause or stop.** dorita980 README: "Note: before dock you need to pause() or stop() your robot." MEDIUM.
- **Firmware OTA.** dorita980 README: block internet access to prevent OTA; "New firmware changes can cause
  dorita980 to stop working." MEDIUM. Relevant to any test on a robot whose firmware may update.
- **Cloud credential handling.** The cloud route needs the iRobot account login, and the cloud returns the local
  password. Keep account credentials out of logs and out of the repo. (Our existing `.env` pattern covers the
  local password; the cloud login is a separate secret.)
- **Silent whole-house clean by a missing map or region.** roombapy's guard (`RoombaScopeError` for empty
  regions) exists because of this failure. Our `CleanRegions` returns `"roomba: no region to clean"` when
  `len(regions) == 0` (`pkg/roomba/client.go` line 222, read 2026-10-29), which matches roombapy's guard. (Our code, not sourced.)
- **Probing guessed ids: no community source reports a result.** Any probe would be a first observation. Do
  not treat that as "safe" or "unsafe".

## 6. What the official sources say (and do not)

- iRobot publishes **no** local API documentation found. iRobot support articles cover the Smart Map UI only
  (see §3d). GAP for any official local-API doc.
- The iRobot support pages do not mention MQTT, the local password, or region ids.
- Official HA integration docs (roomba, read 2026-10-29) cover local MQTT for the integration, not room cleaning.

## 7. Comparison with our code and `docs/vacuum-plan.md`

| our claim (plan / code) | what sources say | agrees? |
|---|---|---|
| Region ids are learned from `lastCommand` after an app room clean | dorita980 README, HA forum (bobrippling 2024; bradymholt 2021) | Yes (MEDIUM-HIGH) |
| `pmaps` in state is `pmap_id -> user_pmapv_id` | Roomba980-Python README example: `pmaps: [{<pmap_id>: <user_pmapv_id>}]` | Yes |
| Room clean sends `pmap_id`, `user_pmapv_id`, `ordered`, `regions[{region_id,type}]` | dorita980 README, Roomba980-Python code, irbt (`ordered:0`) | Yes, but `ordered` 0/1 conflicts |
| Password = longest printable run ≥ 20; ≤ 7 bytes = button not pressed (`password.go`) | roombapy: 7-byte constant may mean cloud-only; fixed byte-7 offset | **Not reconciled.** Flag for review |
| First connect refused; retry 3-5 s later works (6/6) | not documented elsewhere | Measured only |
| CN check on the robot cert | no community client checks CN (all use CERT_NONE) | Our own choice, stricter than all sources read |
| Map version changes "whenever the map is edited" | HA forum (mehalter): changes after runs; iRobot: edit requires remap | Trigger **unsourced** |
| `Edmond` on firmware 3.x | 3.20.7 password change and pose loss documented by Roomba980-Python | Check `softwareVer` on Edmond |
| Cycle/phase values | community lists; our code comments | Partly (see §2) |

## When we looked

All fetches and searches: **2026-10-29**. Source versions: dorita980 `master` (raw README and `lib/v2/local.js`,
`lib/v2/cloud.js` stub, `lib/v1/cloud.js`, `bin/getPasswordCloud.js`) as served that day; Roomba980-Python `master`
README and `roomba/roomba.py`; roombapy `main` (`roomba.py`, `getpassword.py`); tidalf/irbt `master`
(`robot.py`, `cloud.py`, `parse_command_line.py`, `docs/endpoints.md`, `tests/cloud_mock.py`, README); johnnyh1975/ha_roomba_plus
`main` (README, `ARCHITECTURE.md`, tree); HA roomba integration page and core `manifest.json` (roombapy 2.0.2);
HA community threads (dates as stated in §1–§4); packetmischief.ca post (2023-01-15); iRobot support pages
(en_GB 64103; de/es/co.uk search snippets for 64102/64103). Local: `docs/vacuum-plan.md`, `pkg/roomba/state.go`,
`pkg/roomba/client.go`, `pkg/roomba/password.go`, `pkg/roomba/tls.go` in this worktree.

## Related, not asked

- **Roomba+ (johnnyh1975)** is a cloud-first HACS integration for Prime-era models (2026). Its wire keys
  (`start`, `region_id`) for Prime differ from the classic local form; not relevant to our i7 local path.
- **roombapy's `RoombaScopeError` guard** is a usable design reference for "never send an empty region list". Not a design.
- **irbt `docs/endpoints.md`** mentions an `auth` parameter on the `associations` call; header names not recorded.
- **Our `CleanRegions` guard on empty regions** matches roombapy's current behaviour. Noted, not evaluated.

## Gaps / not found

- **`mssnStrtTm` and `pmapLearningTimes`:** searched directly. No source names them. Not documented anywhere we could reach.
- **Full i7 shadow key list with meanings:** no official list; community sources only; no source documents every key.
- **Does a local `start` with `regions` require `user_pmapv_id` to match the robot's current version?** Not stated. Only johnnyh1975's single report.
- **Region-id stability across map edits, app re-learning, and whole-house cleans:** no source gives a tested answer.
  iRobot's own article contradicts itself on divider edits (§3d).
- **Behaviour of an unknown `region_id`:** not documented.
- **Meaning of `ordered` 0 vs 1 for a single room:** conflicting sources, no authority.
- **`initiator` value accepted for a local room clean:** `localApp` (ours, dorita980) vs `rmtApp` (irbt, HA 2024). Not established which is required.
- **Official iRobot release notes for firmware 3.x or 3.20.7 changes:** not found.
- **The exact 3.20.7 password workaround** (local or cloud timing and bytes): README names the cloud route only.
- **roombapy PyPI release notes:** the PyPI page did not load on fetch; the empty-regions behaviour is taken from the
  search snippet of that page **and** from the roombapy `main` source comment.
- **i7-specific Smart Map article:** the iRobot support article read covers i3/i4/i5; the i7 article was not found.
- **irbt transport for its `_cmd`** (cloud vs local): not read; the file's `_cmd` is not inspected.
- **Phases `hmUsrDock`/`hmMidMsn`/etc. on Edmond:** taken from our code comments; not re-measured here.
- **Reddit (r/roomba, r/homeassistant) and GitHub issues:** not searched in depth (time budget). The dorita980 and roombapy issue trackers were not read.
- **Our `pkg/roomba/password.go`:** only the parsing function and its comment were read (lines 22-40). `tls.go` was not read; the CN check is described from vacuum-plan.md.
- **The 7-byte reply meaning** (§4c): not reconciled.

## Verified on our account, 2026-10-29

Run once, read-only, with `irobot_cloud_rooms.py` (stdlib + requests, flow from tidalf/irbt, country `FR`).
No retry, nothing written, no credential printed.

| step | result |
|---|---|
| `GET disc-prod.iot.irobotapi.com/v1/app/discover?country_code=FR` | 200, has `gigya`, `httpBaseAuth`, `httpBase`, `awsRegion` |
| Gigya `accounts.login` (us1) | 200, `errorCode` 0 |
| `POST {httpBase}/v1/login/account` (`assume_robot_ownership: "0"`) | 200, AWS credentials returned |
| SigV4 `GET {httpBaseAuth}/v1/user/associations/robots` | 200; the account holds exactly our robot |
| SigV4 `GET .../v1/<blid>/pmaps?activeDetails=1` | 200; **2 maps** |

So the irbt login chain (not dorita980's `/v2/login`) works as of today: **HIGH** for the chain and
the `pmaps` shape; the discovery path `/v1/app/discover` is the one that answered.

Map `e8EOWqiwQ6e8sGpyYU4C-w` (the one the robot reports locally), name "Rez-de-chaussée": 8 regions in
`active_pmapv_details.regions[]`, each `{id, name, region_type}`, no `type` field:

| region id | name | region_type |
|---|---|---|
| 1 | Salle à manger | dining_room |
| 8 | Chambre à coucher | bedroom |
| 16 | Buanderie | laundry_room |
| 17 | Salle de bains | bathroom |
| 18 | Couloir | hallway |
| 19 | Salle de séjour | family_room |
| 20 | Salon | living_room |
| 21 | Cuisine | kitchen |

The second map (`Rp-tEHCuQuGwH7cd0jKdnQ`, version 231016T113248) returned no regions.

**Version mismatch, open (MEDIUM concern):** the cloud's `user_pmapv_id` for the first map is
`251027T114719`, while the robot's own `pmaps` state reports `260829T114918` for the same map id. The
robot's local value is the newer one. Region ids come from the older cloud version, so they may not
match the map the robot now uses. Room cleans must send the robot's **local** `user_pmapv_id`. Whether
the ids are still valid is unverified until a single-room clean is tried (see §5 on the unknown effect of
a wrong id). Also unverified: that cloud ids equal local ids (irbt's behaviour only suggests it).
