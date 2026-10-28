# Robot vacuum (iRobot Roomba)

MyHome controls iRobot Roomba vacuums **directly on the LAN**, with no iRobot cloud: the robot
runs its own MQTT broker on port 8883, and the daemon keeps one connection to it. This keeps
working when the internet is down, and when the iRobot app shows the robot as "offline".

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
myhome ctl vacuum rooms                     # learned rooms
myhome ctl vacuum rooms name 11 kitchen     # name a learned room
```

`--robot <name|id>` selects a robot, and can be omitted (the daemon has one configured robot). A command
reports success only once the robot has **acknowledged** it, by echoing it back (normally
within 1–3 s).

The web UI shows a **Vacuum** section on the main page once a robot is configured. It has the
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
| Room "learned on an older version of the map" | The map changed in the app. Start that room once from the app. |

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
