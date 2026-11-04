# Fix: devices get MQTT broker overwritten with loopback address

## Bug

Some Shelly devices intermittently end up with their MQTT broker config set
to `localhost:1883` (which the device itself resolves to `127.0.0.1`),
breaking MQTT connectivity until manually reconfigured.

## Root cause (three compounding issues)

1. `myhome/devices/impl/manager.go` (daemon `Start()`) builds the auto-setup
   MQTT broker from `dm.mqttClient.GetServer()`, which is the daemon's own
   connection string (literally `"localhost:1883"` when running the default
   embedded broker). It does not go through `DeviceServer()`, the
   loopback→LAN-IP resolver already added for the CLI paths
   (`myhome ctl shelly setup`, `myhome ctl shelly mqtt config`) in commit
   `a52aeba`.
2. `myhome/daemon/watch/zeroconf.go` treats *any* error from
   `db.GetDeviceByAny` the same as "device not in DB yet", rebuilding a fresh
   in-memory device with `Info == nil`. That flag makes
   `manager.go`'s `deviceUpdaterLoop` treat an already-configured device as
   brand new and run auto-setup on it again.
3. `internal/myhome/shelly/setup/setup.go`'s `IsDeviceSetUp` collapses *any*
   error (communication failure, timeout, no channel available) into
   `false` ("not set up"), so a transient hiccup talking to an already-setup
   device causes the full setup flow — including the MQTT broker rewrite —
   to run again.

Any one of #2 or #3 misfiring, combined with #1, rewrites a working device's
broker to the unresolved loopback string.

## Fix

- [x] Phase 1: `manager.go` — use `mc.DeviceServer()` (loopback-resolved)
      instead of `mc.GetServer()` for the auto-setup MQTT broker, with
      fallback to the existing `options.Flags.MqttBroker` / `mqtt.local`
      chain if resolution fails.
- [x] Phase 2: `zeroconf.go` — only treat a device as unknown when the DB
      lookup says "not found" (`sql.ErrNoRows`); on any other DB error, skip
      this discovery entry instead of fabricating a fresh "new" device.
- [x] Phase 3: `setup.go` / `manager.go` — `IsDeviceSetUp` returns
      `(bool, error)`. The error case is now reserved for "no channel
      available at all" (`selectChannel` failure) — genuinely ambiguous,
      and `SetupDevice` would fail at the same step anyway, so callers skip
      this round rather than treating it as "not set up."

      A KVS read failure (the actual `script/setup/done` lookup) still
      can't be cleanly split into "genuinely missing key" vs "transient
      comm error": the MQTT and HTTP RPC transports
      (`pkg/shelly/mqtt/channel.go`, `pkg/shelly/shttp/channel.go`) collapse
      both into a generic `error`, and the HTTP channel doesn't even decode
      the JSON-RPC error envelope at all today. Properly fixing that needs
      transport-level changes (capturing `res.Error.Code`, parsing it on the
      HTTP side too) that are out of scope for this bug and were not
      attempted here to avoid hardcoding an unverified Shelly RPC error code
      as load-bearing logic. As a proportionate mitigation, `IsDeviceSetUp`
      retries the KVS read once (1s apart) before concluding "not set up" —
      reduces, but does not eliminate, the chance a single flaky read
      triggers a full re-setup (incl. MQTT broker rewrite) on a working
      device. Phase 2 already removes the dominant real-world trigger for
      that scenario.

No transport-level RPC error-code plumbing needed — these are all
caller-side fixes that stop collapsing "unknown" into "definitely not set
up / definitely new."

- [x] Phase 4: regular reconciliation job (self-healing safety net,
      independent of root cause above).

  `myhome/devices/impl/manager.go`'s new `runReconciliationLoop` (ticker
  pattern modeled on `runDeviceRefreshJob`) walks every known Gen2+ device
  (skip Gen1/BLU, same filter as the refresh job) once per
  `ReconcileInterval` and calls the new
  `internal/myhome/shelly/setup/setup.go:ReconcileConfig`, which **forces
  HTTP** (not MQTT — if a device's broker is wrong, MQTT may falsely look
  "ready" while not actually reaching the daemon, so HTTP-to-the-device's-
  own-IP is the only channel guaranteed to reflect reality) and re-applies:
  - MQTT broker = the canonical, loopback-resolved broker address
    (`dm.setupConfig`, the same source of truth built for auto-setup in
    Phase 1, now also reused here).
  - NTP server (`pool.ntp.org`) and Matter disabled — the other idempotent
    values `SetupDevice` establishes on first setup.

  `ReconcileConfig` explicitly does **not** touch the device name, WiFi, or
  watchdog.js script management — those are one-time provisioning concerns,
  and re-running them periodically risks clobbering user edits (e.g. a
  renamed device) for no benefit. It only issues a write (and the
  subsequent reboot-and-wait) when the current value actually differs from
  the canonical one, so a healthy device sees nothing but a few idempotent
  reads each cycle.

  Implementation extracted two pieces of `SetupDevice` into reusable
  helpers rather than threading a channel parameter through it directly
  (lower risk, no behavior change to the existing CLI-driven setup path):
  `resolveMqttServer` (broker string resolution) and `rebootIfRequired`
  (reboot-and-wait-for-online loop) in `setup.go`.

  New config: `ReconcileInterval` (flag `--reconcile-interval`, env
  `MYHOME_DAEMON_RECONCILE_INTERVAL`, default `1h`) — added to
  `options.go`, `run.go`, `docs/configuration.md`, `myhome-example.yaml`
  per the project's 4-file config-option convention. `0` disables the loop.
