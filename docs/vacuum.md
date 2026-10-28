# Robot vacuum (iRobot Roomba)

MyHome controls iRobot Roomba vacuums **directly on the LAN**, with no iRobot cloud: the robot
runs its own MQTT broker on port 8883, and the daemon keeps one connection to it. This keeps
working when the internet is down, and when the iRobot app shows the robot as "offline".

Tested on a Roomba i7 (SKU i715640, firmware `lewis+22.52.10`). Other Wi-Fi Roombas that
dorita980 supports should work too, but have not been tried. Design and field measurements:
[vacuum-plan.md](vacuum-plan.md). Epic: #606.

## Pairing (once per robot)

The daemon needs the robot's local password, which the robot only hands out right after a
button press:

1. Put the robot on its dock. If it has been off the dock for a while, press CLEAN to wake it,
   then dock it again.
2. Hold its **HOME** button about 2 seconds, until it plays a tone.
3. Within 2 minutes, run:

   ```bash
   myhome ctl vacuum pair             # finds the robot by discovery
   myhome ctl vacuum pair 192.168.1.50 # or name its IP if several robots answer
   ```

The handshake runs **on the daemon**, and the password stays in `myhome.db` on the daemon host.
It never travels over `myhome/rpc`, and is never logged or printed. The password stays valid
until the robot is factory-reset. If the robot later rejects it, the daemon logs
"Robot rejected its stored password; pair it again".

A robot accepts **one local client at a time**: pair it with one daemon only. A development
daemon that holds the same pairing will fight the production one for the connection.

## Using it

```bash
myhome ctl vacuum list                      # paired robots and their state
myhome ctl vacuum status
myhome ctl vacuum clean                     # whole home
myhome ctl vacuum clean --room kitchen --room hall   # those rooms, in order
myhome ctl vacuum pause | resume | stop | dock | find
myhome ctl vacuum rooms                     # learned rooms
myhome ctl vacuum rooms name 11 kitchen     # name a learned room
```

`--robot <name|id>` selects a robot, and can be omitted when only one is paired. A command
reports success only once the robot has **acknowledged** it, by echoing it back (normally
within 1–3 s).

The web UI shows a **Vacuum** section on the main page once a robot is paired. It has the
robot's state, battery, bin, errors, the controls that make sense for what the robot is doing,
and a room picker.

## Rooms

The robot addresses rooms by ids that exist only on the robot's map. MyHome **learns** them:
whenever a room clean is started, from MyHome or from the iRobot app, the robot reports the
map and region ids it used, and the daemon records them.

To set up rooms once:

1. In the iRobot app, start a clean of one room (or several), then stop it.
2. `myhome ctl vacuum rooms` now lists the region ids. Name them with `rooms name`.

When the map is edited in the app, its version changes. Rooms learned on the older version are
then marked **stale** and refused, rather than risking cleaning the wrong place. Start a room
clean from the app again to refresh them.

## Finding the robot

The robot's address comes from DHCP and **does change**: it has moved from .74 to .50 before.
The daemon never uses a configured IP. Every time it (re)connects, it looks the robot up by its
id with a UDP broadcast, and falls back to the last address that worked. The UDP broadcast can
go unanswered, especially while the robot is driving. The TLS certificate must name the
expected robot (`CN=iRobot-<id>`), so an address now held by another device is rejected.

A DHCP reservation on the router helps, but is not required. The SFR box has no API for it
(web UI only).

## Troubleshooting

| symptom | cause |
|---|---|
| `pair`: "hold the robot's HOME button…" | The button was not held, or more than ~2 minutes passed. Press again and retry. |
| First connection attempt refused | Normal: the robot refuses a connection that follows another too closely, and the retry 3–5 s later succeeds. |
| `not connected (last known address …)` | The robot is asleep off the dock, or out of Wi-Fi range. Dock it, or press CLEAN. |
| A command "not acknowledged by the robot" | The robot did not echo the command within 10 s. Check `status`. |
| Room "learned on an older version of the map" | The map changed in the app. Start that room once from the app. |

## Live test

`pkg/roomba` has a live test against a real robot (not part of `make test`). It reads the
robot's state and sends `dock`:

```bash
ROOMBA_CREDS=path/to/creds.json go test -tags live -run TestLive -v ./pkg/roomba
```

`creds.json` is `{"blid": "…", "password": "…"}`; keep it out of git, with mode 0600.

## Developer notes (Edmond)

The robot used during development is a Roomba i7 named **Edmond** (epic #606, PR #622).

- **Credentials.** Its id and local password are in `~/.homepi/roomba-creds.json` on the Mac used
  for development (mode 0600). They are never committed, logged, or put in an issue. To work from
  another machine, copy that file over a private channel (e.g. `scp`), or press HOME and pair
  again: the password only changes on a factory reset.
- **Address.** DHCP, last seen at 192.168.1.50 (previously .74). See "Finding the robot" above.
- **One client at a time.** Stop any development daemon that holds Edmond before the NAS daemon
  pairs with it, and the other way round. Two daemons fight for the single connection.
- **Running a development daemon from a directory that contains a file named `myhome`** (e.g. the
  binary itself) fails at startup with `yaml: invalid trailing UTF-8 octet`: the daemon reads
  that file as its config. Name the binary something else (e.g. `myhome-bin`). Tracked in #623.
