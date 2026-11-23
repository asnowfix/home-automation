# BLU relay coverage, from `events.db` (#850)

Several Shelly devices run `blu-publisher.js` and forward the same BLE advertisement of a BLU sensor
to `shelly-blu/events/<MAC>`. Since #850:

- each publisher adds its own device id to the payload as `"relay": "<id>"` (read once at script
  start from `Shelly.getDeviceInfo().id`);
- the daemon (`internal/myhome/shelly/blu/dedup.go`) treats messages with the same
  `(address, pid, event)` within **5 seconds** as one physical event. The first copy is inserted at
  once (the event log and its SSE broadcast are not delayed); later copies amend that row's `data`.

The `data` of every BLU `motion.*`, `window.*`, `button.push` and `battery.low` row now carries:

```json
{"motion": 1, "address": "aa:bb:cc:00:11:22", "rssi": -72,
 "relays": {"relay-a": -72, "relay-b": -90}}
```

- `relays` maps each relay that heard the event to the rssi it measured (best rssi if a relay
  heard it more than once). `rssi` at the top level is the best of them.
- Copies from publishers that predate the `relay` key are recorded under the key `"unknown"`
  (best rssi among them). They count as **one** relay in the queries below, so coverage of a sensor
  still heard through old publishers is under-estimated until every publisher is updated.
- Rows recorded before #850 have no `relays` key and are excluded by the queries.
- A packet without a `pid` is never merged.

Degraded mode: the de-duplication only shapes what is recorded. Without the daemon, devices keep
working exactly as before (`blu-listener.js` and `heater*.js` read the MQTT message directly) and
nothing is lost except this measurement.

## Share of events heard by 1, 2, 3+ relays, per sensor

```sql
WITH ev AS (
  SELECT device_id,
         (SELECT COUNT(*) FROM json_each(events.data, '$.relays')) AS n
  FROM events
  WHERE json_type(data, '$.relays') = 'object'
    AND ts > strftime('%s', 'now', '-14 days')
)
SELECT device_id,
       COUNT(*)                                       AS events,
       ROUND(100.0 * SUM(n = 1)  / COUNT(*), 1)       AS pct_1_relay,
       ROUND(100.0 * SUM(n = 2)  / COUNT(*), 1)       AS pct_2_relays,
       ROUND(100.0 * SUM(n >= 3) / COUNT(*), 1)       AS pct_3plus_relays
FROM ev
GROUP BY device_id
ORDER BY events DESC;
```

## Per-relay rssi, per sensor

Which relay hears a sensor, how often, and how well: use it to decide where a new relay would help.

```sql
SELECT e.device_id,
       r.key                          AS relay,
       COUNT(*)                       AS heard,
       ROUND(AVG(r.value), 1)         AS avg_rssi,
       MIN(r.value)                   AS worst_rssi,
       MAX(r.value)                   AS best_rssi
FROM events e, json_each(e.data, '$.relays') AS r
WHERE json_type(e.data, '$.relays') = 'object'
  AND e.ts > strftime('%s', 'now', '-14 days')
GROUP BY e.device_id, r.key
ORDER BY e.device_id, heard DESC;
```

Run either against a copy of the file, e.g. `sqlite3 -readonly events.db < query.sql` (on the NAS,
which has no `sqlite3`, use `python3`'s `sqlite3` module; the JSON functions need SQLite 3.38+ or a
build with JSON1). Remember the 90-day retention of `events.db`.
