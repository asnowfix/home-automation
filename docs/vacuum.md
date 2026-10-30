# Robot vacuum (iRobot Roomba)

MyHome controls iRobot Roomba vacuums **directly on the LAN**, with no iRobot cloud: the robot
runs its own MQTT broker on port 8883, and the daemon keeps one connection to it. This keeps
working when the internet is down, and when the iRobot app shows the robot as "offline".

The one optional use of the iRobot cloud is to read the robot's **rooms**: names and ids. The robot
does not report them locally (see [Rooms](#rooms)). Only that read needs the internet, and control
never depends on it.

Tested on a Roomba i7 (SKU i715640, firmware `lewis+22.52.10`). Other Wi-Fi Roombas that
dorita980 supports should work too, but have not been tried. Design and field measurements:
[vacuum-plan.md](vacuum-plan.md). Epic: #606.

## Configuration (once per robot)

The daemon reads the robot's id (BLID) and local password from its environment, like the Beem
and SFR credentials (#626): `MYHOME_VACUUM_BLID` and `MYHOME_VACUUM_PASSWORD`, plus the
optional `MYHOME_VACUUM_HOST`. They belong in the daemon's `.env` (`/var/lib/myhome/.env` on the
NAS), never on the command line and never in `myhome.yaml`; the password is not stored in
`myhome.db`. Without both, the daemon logs "Robot vacuum integration disabled" and the vacuum
commands answer "no robot vacuum configured". Reference: [configuration.md](configuration.md#robot-vacuum).

The robot only hands out its password right after a button press. To get both values:

1. Put the robot on its dock. If it has been off the dock for a while, press CLEAN to wake it,
   then dock it again.
2. Hold its **HOME** button about 2 seconds, until it plays a tone.
3. Within 2 minutes, on a host on the robot's LAN, run:

   ```bash
   myhome ctl vacuum pair              # finds the robot by discovery
   myhome ctl vacuum pair 192.168.1.50 # or name its IP if several robots answer
   ```

   It prints, on standard output only:

   ```
   MYHOME_VACUUM_BLID=…
   MYHOME_VACUUM_PASSWORD=…
   MYHOME_VACUUM_HOST=…     (only when you named the IP)
   ```

4. Put those lines in the daemon's `.env` (on the NAS, `sudo dpkg-reconfigure myhome` prompts
   for them), then restart the daemon (`sudo systemctl restart myhome`).

`pair` talks to the robot directly, not through the daemon, so the password never travels over
`myhome/rpc`. It writes nothing anywhere: the printed lines are the only copy, so do not paste
them into logs, issues or chats. The daemon never logs the password, returns it over RPC, or
stores it. The password stays valid until the robot is factory-reset. If the robot later
rejects it, the daemon logs "Robot rejected MYHOME_VACUUM_PASSWORD; run `myhome ctl vacuum pair`
and update .env".

A robot accepts **one local client at a time**: configure it in one daemon only. A development
daemon configured with the same robot will fight the production one for the connection, and
`pair` itself fails while a daemon holds the connection.

## Using it

```bash
myhome ctl vacuum list                      # the configured robot and its state
myhome ctl vacuum status
myhome ctl vacuum clean                     # whole home
myhome ctl vacuum clean --room kitchen --room hall   # those rooms, in order
myhome ctl vacuum pause | resume | stop | dock | find
myhome ctl vacuum rooms                     # known rooms, with where each came from
myhome ctl vacuum rooms sync                # fetch the rooms from the iRobot cloud account
myhome ctl vacuum rooms name 11 kitchen     # name a room the cloud did not provide
```

`--robot <name|id>` selects a robot, and can be omitted (the daemon has one configured robot). A command
reports success only once the robot has **acknowledged** it, by echoing it back (normally
within 1–3 s).

The web UI shows a **Vacuum** section on the main page once a robot is configured. It has the
robot's state, battery, bin, errors, the controls that make sense for what the robot is doing,
and a room picker.

## Rooms

The robot addresses rooms by **region ids** that exist only on its map, and it does not report
its rooms over the LAN: its local state has no room list and no names (checked 2026-10-29 on
Edmond). There are two ways to get them.

### From the iRobot cloud account (preferred)

Set the iRobot **app account** in the daemon's `.env` (`sudo dpkg-reconfigure myhome` prompts
for it; reference: [configuration.md](configuration.md#irobot-cloud-account-room-names)):

```
MYHOME_IROBOT_EMAIL=…
MYHOME_IROBOT_PASSWORD=…
```

These are the account's credentials, not the robot's: `MYHOME_VACUUM_*` (above) stay the robot's
own local credentials, and the robot must be configured too. Only plain email+password accounts
work: an account that signs in with Google or Apple, or that has two-factor authentication, is
refused with a message saying so.

The daemon then fetches the rooms **once at start**, in the background. `myhome ctl vacuum rooms
sync` fetches them again, for example after editing the map in the app:

```
$ myhome ctl vacuum rooms sync
map "Rez-de-chaussée" (e8EO…): 8 rooms, cloud version 251027T114719, robot version 261029T090306
0 added, 8 updated, 0 now stale. See 'myhome ctl vacuum rooms'.
```

What a sync does:

- It logs in **once** (discovery, Gigya login, iRobot login, then the map read). It does not retry,
  so a wrong password cannot lock the account. On any failure it names the failing step (and, for
  the Gigya login, Gigya's `errorCode`), logs a warning, and **keeps the rooms already stored**.
  Each HTTP call has a timeout. The daemon never waits for the cloud to start, and there is no
  periodic sync.
- **The cloud is the source of truth for room names**, because no usable rename API is known for
  this robot (see [irobot-api-verbs.md](irobot-api-verbs.md); owner decision, #628). That is "not
  found", not "proven absent": the official app can rename rooms, but its call is undocumented. A
  sync overwrites local names. A room the cloud no longer lists is
  marked **stale** and refused, not deleted. While the account is configured, `rooms name` refuses
  rooms that came from the cloud ("managed by the iRobot cloud"). Rename them in the app, then sync.
- It reads nothing else: not the robot's password, not map geometry, not schedules. It writes
  nothing to the cloud.

**Map versions: room cleans name the cloud's.** The cloud stores the rooms of the map version last
confirmed in the app. The robot's own reported version moves on after every clean. On 2026-10-29 the
cloud held `251027T114719` while the robot reported `261029T090306` for the same map, so a mismatch
is the **normal** state. `rooms` shows both versions, for information only:

```
Map Rez-de-chaussée (e8EO…): room cleans name iRobot cloud map version 251027T114719; the robot reports 261029T090306.
```

A clean of a cloud room names the **cloud's** version. This is a measured result, on one robot
(Edmond, 2026-10-30). The same single-room clean of the kitchen (region 21) was rejected with the
robot's own version (acknowledged, mission opened, aborted within ~3 s with `error 224`, never left
the dock), and it cleaned the kitchen with the cloud's version. The cloud's region id was right.
Hypothesis, **not verified**: the robot reports its latest learned map version, while a clean must
name the version the cloud considers active. Rooms learned from a robot echo are cleaned with that
echo's version. Either way the map id must be one the robot currently reports, or the clean is
refused.

**One automatic retry** (owner decision, 2026-10-30). After the robot acknowledges a room clean, the
daemon watches it for 15 s. An **immediate abort** means the robot is back on the dock (phase
`charge`, cycle `none`) with an error code, without having run in that window. On an immediate abort,
and only then, the daemon retries **once** with the other known version of the same map (cloud
version, or the robot's reported one). It does so only when that version exists and differs, the
robot is still docked, and no stop, pause or dock was sent in the meantime. Never a third attempt.
Both attempts are logged with their versions and error codes. `clean` prints which version ran; if
both aborted it prints both attempts and the error-224 hint, and fails. The cloud-version-first order
comes from that single measurement on one robot. The retry is a constant in `myhome/vacuum/clean.go`
(`retryOtherVersion`) and can be turned off.

`error 224` is not documented anywhere. `status`, the clean output and the web UI explain it as:
"the robot opened the mission and aborted at once; seen when a room clean names a map version the
robot does not accept".

### Learned from the robot (fallback, no account)

Whenever a room clean is started, from MyHome or from the iRobot app, the robot reports the map
and region ids it used, and the daemon records them. Without the account:

1. In the iRobot app, start a clean of one room (or several), then stop it.
2. `myhome ctl vacuum rooms` now lists the region ids. Name them with `rooms name`.

When the robot's map version changes, rooms learned on the older version are marked **stale** and
refused, rather than risking cleaning the wrong place. Start a room clean from the app again to
refresh them. Note that the robot's version changes after cleans, not only after map edits (see
above), so learned rooms go stale often. That is one reason the cloud is preferred. (Whether this
staleness check is even right is open: a room started from the app may echo the cloud's version,
which then never equals the robot's reported one.)

## Finding the robot

The robot's address comes from DHCP and **does change**: it has moved from .74 to .50 before.
By default the daemon uses no configured IP: every time it (re)connects, it looks the robot up
by its id with a UDP broadcast, and falls back to the last address that worked. The UDP
broadcast can go unanswered, especially while the robot is driving.

`MYHOME_VACUUM_HOST` overrides this: when set, the daemon connects to that address and skips
discovery. Use it only with a DHCP reservation, or where broadcast does not reach the robot. The TLS certificate must name the
expected robot (`CN=iRobot-<id>`), so an address now held by another device is rejected.

A DHCP reservation on the router helps, but is not required. The SFR box has no API for it
(web UI only).

## Troubleshooting

| symptom | cause |
|---|---|
| "no robot vacuum configured" | `MYHOME_VACUUM_BLID` or `MYHOME_VACUUM_PASSWORD` is not in the daemon's environment. Check `.env` and restart the daemon. |
| `pair`: "hold the robot's HOME button…" | The button was not held, or more than ~2 minutes passed. Press again and retry. |
| First connection attempt refused | Normal: the robot refuses a connection that follows another too closely, and the retry 3–5 s later succeeds. |
| `not connected (last known address …)` | The robot is asleep off the dock, or out of Wi-Fi range. Dock it, or press CLEAN. |
| A command "not acknowledged by the robot" | The robot did not echo the command within 10 s. Check `status`. |
| `clean`: "aborted it at once … error 224" | The robot rejected the map version named. Rooms from the cloud already use the cloud version and get one retry with the robot's; if both abort, run `rooms sync` (the map may have changed in the app) and check `status`. |
| `clean`: "does not report map …" | The robot is not connected yet, or the map no longer exists on it. |
| Room "learned on an older version of the map" | The robot's map version changed. Run `rooms sync` (with the iRobot account), or start that room once from the app. |
| `rooms sync`: "not configured" | `MYHOME_IROBOT_EMAIL` or `MYHOME_IROBOT_PASSWORD` is not in the daemon's environment. |
| `rooms sync`: "gigya login failed (Gigya errorCode 403042)" | Wrong account email or password. One attempt is made per sync; fix `.env` and restart before trying again. |
| `rooms sync`: "only plain email+password accounts are supported" | The account signs in with Google/Apple, or uses two-factor. Not supported. |
| `rooms sync`: "discover failed" / timeout | No internet, or the iRobot cloud is down. The stored rooms are kept; control is unaffected. |
| `rooms name`: "managed by the iRobot cloud" | The room came from the cloud, which owns its name. Rename it in the app, then `rooms sync`. |
| Room "no longer on the iRobot cloud map" | The last sync did not list it (the map was edited in the app). |

## Live test

`pkg/roomba` has a live test against a real robot (not part of `make test`). It reads the
robot's state and sends `dock`:

```bash
set -a; . ./.env; set +a     # MYHOME_VACUUM_BLID, MYHOME_VACUUM_PASSWORD (see example.env)
go test -tags live -run TestLive -v ./pkg/roomba
```

It reads the same variables as the daemon; `MYHOME_VACUUM_HOST` skips discovery. `.env` is
git-ignored; keep it mode 0600. The test skips when the variables are unset, and never prints
the password.

## Developer notes (Edmond)

The robot used during development is a Roomba i7 named **Edmond** (epic #606, PR #622).

- **API reference.** [irobot-api-verbs.md](irobot-api-verbs.md) catalogs the local and cloud verbs
  and records which ones we verified. [irobot-local-api-research.md](irobot-local-api-research.md)
  has the sources and confidence levels behind them.

- **Credentials.** Its id and local password are the `MYHOME_VACUUM_BLID` /
  `MYHOME_VACUUM_PASSWORD` lines of the daemon's `.env`: `/var/lib/myhome/.env` on the NAS, and the
  git-ignored `.env` at the repo root on a development machine (see `example.env`). They are
  never committed, logged, or put in an issue. To work from another machine, copy those two lines
  over a private channel, or press HOME and run `myhome ctl vacuum pair` again: the password only
  changes on a factory reset.
- **Address.** DHCP, last seen at 192.168.1.50 (previously .74). See "Finding the robot" above.
- **One client at a time.** Stop any development daemon configured with Edmond before the NAS
  daemon connects to it (and before running `pair` or the live test), and the other way round.
  Two clients fight for the single connection.
- **Running a development daemon from a directory that contains a file named `myhome`** (e.g. the
  binary itself) fails at startup with `yaml: invalid trailing UTF-8 octet`: the daemon reads
  that file as its config. Name the binary something else (e.g. `myhome-bin`). Tracked in #623.
