# MyHome Configuration Guide

## Overview

MyHome uses a hierarchical configuration system with three levels of precedence:

1. **Command-line flags** (highest priority)
2. **Environment variables** (middle priority)
3. **Configuration file** (lowest priority)

This allows flexible deployment scenarios from development to production.

> **Env var naming**: all config keys are reachable as `MYHOME_<SECTION>_<KEY>` (e.g. `daemon.mqtt_timeout` → `MYHOME_DAEMON_MQTT_TIMEOUT`) via `AutomaticEnv`. Only account credentials are explicitly documented with their env var name.

## Configuration File

### Location

MyHome searches for `myhome.yaml` in the following locations (in order):

1. `.` (current directory)
2. `/etc/myhome/`
3. `$HOME/.myhome/`

The first file found is used.

### Format

Configuration uses YAML format with two main sections:

- `daemon`: Daemon runtime configuration
- `temperatures`: Temperature service configuration

## Daemon Configuration

### Complete Example

```yaml
daemon:
  # MQTT Broker Configuration
  mqtt_broker: ""  # Empty = use embedded broker
  
  # Timeouts and Intervals
  mdns_timeout: 7s
  mqtt_timeout: 14s
  mqtt_grace: 2s
  refresh_interval: 1m
  mqtt_watchdog_interval: 30s
  mqtt_watchdog_max_failures: 3
  mqtt_reconnect_interval: 2h
  mqtt_broker_client_log_interval: 2m
  
  # MyHome Ports
  ui_port: 6080
  
  # Service Enablement
  enable_gen1_proxy: false
  enable_occupancy_service: false
  enable_temperature_service: false
  
  # Device Manager
  disable_device_manager: false
  
  # Event Logging
  events_dir: ""
```

### Configuration Options

#### MQTT Broker

**`mqtt_broker`** (string, default: `""`)
- MQTT broker URL for device communication
- Empty string = use embedded broker (auto-discovered)
- Example: `"mqtt://192.168.1.100:1883"`
- Flag: `--mqtt-broker` or `-B`

#### Timeouts and Intervals

**`mdns_timeout`** (duration, default: `7s`)
- Timeout for mDNS lookups
- Flag: `--mdns-timeout` or `-M`

**`mqtt_timeout`** (duration, default: `14s`)
- Timeout for MQTT operations
- Flag: `--mqtt-timeout` or `-T`

**`mqtt_grace`** (duration, default: `2s`)
- MQTT disconnection grace period
- Flag: `--mqtt-grace` or `-G`

**`refresh_interval`** (duration, default: `1m`)
- Known devices refresh interval
- Flag: `--refresh-interval` or `-R`

**`mqtt_watchdog_interval`** (duration, default: `30s`)
- MQTT watchdog check interval
- Flag: `--mqtt-watchdog-interval` or `-W`

**`mqtt_watchdog_max_failures`** (int, default: `3`)
- Max consecutive failures before restart
- Flag: `--mqtt-watchdog-max-failures` or `-F`

**`mqtt_reconnect_interval`** (duration, default: `2h`)
- Interval for periodic MQTT reconnection to refresh retained messages
- Useful after suspend/resume cycles to ensure latest device states
- Set to `0` to disable periodic reconnection
- Flag: `--mqtt-reconnect-interval`

**`mqtt_broker_client_log_interval`** (duration, default: `2m`)
- Interval for logging MQTT broker connected clients
- Set to `0` to disable
- Flag: `--mqtt-broker-client-log-interval`

#### Service Ports

**`ui_port`** (int, default: `6080`)
- UI listen port
- Flag: `--ui-port` or `-u`

**`remote_proxy`** (string, default: `""`)
- Forward all `/devices/...` HTTP requests to a remote myhome daemon instead of connecting to devices directly. Useful when running a local myhome instance that reaches the home network via SSH port-forwarding and cannot dial device IPs directly.
- Example: `http://home-pi:6080` or `http://localhost:6081` (when `ssh -L 6081:localhost:6080 home-pi`)
- Flag: `--remote-proxy`

#### Service Enablement

**`enable_gen1_proxy`** (bool, default: auto)
- Enable Gen1 HTTP->MQTT proxy
- Auto-enabled when using embedded broker
- Flag: `--enable-gen1-proxy` / `--disable-gen1-proxy`

**`enable_occupancy_service`** (bool, default: auto)
- Enable occupancy detection service (port 8889)
- Auto-enabled when using embedded broker
- Flag: `--enable-occupancy-service` / `--disable-occupancy-service`

**`enable_temperature_service`** (bool, default: auto)
- Enable temperature scheduling service (port 8890)
- Auto-enabled when using embedded broker
- Requires `temperatures` section in config
- Flag: `--enable-temperature-service` / `--disable-temperature-service`

#### Device Manager

**`disable_device_manager`** (bool, default: `false`)
- Disable the device manager
- Flag: `--disable-device-manager` or `-D`

#### Event Logging

**`events_dir`** (string, default: `""`)
- Directory to write received MQTT events as JSON files
- Empty = disabled
- Flag: `--events-dir` or `-E`

## Events Configuration

The event log service records every meaningful state-change event from all devices to a dedicated SQLite database, with optional auto-purge and live CLI tailing.

### Example

```yaml
events:
  db: ~/.myhome/events.db    # path to events SQLite database (separate from devices.db)
  retention: 2160h           # auto-purge threshold; events older than this are deleted (default 90 days)
  enabled: true              # set false to disable event recording entirely
```

### Options

**`db`** (string, default: `~/.myhome/events.db`)
- Path to the events SQLite database file
- Kept separate from `devices.db` to allow independent backup and rotation
- Flag: `--events-db`

**`retention`** (duration, default: `2160h`)
- How long events are kept before automatic deletion (90 days by default)
- Purge runs hourly; only the `events` table is purged (sensor daily stats are kept indefinitely)
- Set to `0` to disable automatic purging
- Flag: `--events-retention`

**`enabled`** (bool, default: `true`)
- Set to `false` to disable the event recording service entirely
- Flag: `--enable-events-service` / `--disable-events-service`

### CLI Commands

#### `myhome ctl events list`

Query historical events from the database.

```
myhome ctl events list
    [--device <id|name|mac>]   filter by device
    [--type <event-prefix>]    e.g. "switch" matches switch.on + switch.off
    [--severity <level>]       alarm|warn|info|debug
    [--since <duration>]       e.g. 24h, 7d (default: 24h)
    [--limit <n>]              max rows (default: 100)
    [--json]                   machine-readable output
```

#### `myhome ctl events follow`

Tail live events via SSE stream (real-time output).

```
myhome ctl events follow
    [--device <id|name|mac>]   filter by device
    [--type <event-prefix>]    filter by event type prefix
    [--severity <level>]       default: info+warn+alarm
```

#### `myhome ctl events clear`

Delete events from the database.

```
myhome ctl events clear
    [--before <RFC3339 | duration>]   default: retention threshold
    [--dry-run]                       show what would be deleted without deleting
```

## Temperature Configuration

### Example

```yaml
temperatures:
  port: 8890
  rooms:
    living-room:
      name: "Living Room"
      comfort_temp: 21.0
      eco_temp: 17.0
      schedule:
        weekday: ["06:00-23:00"]
        weekend: ["08:00-23:00"]
```

### Philosophy

**Eco is the default** - only define comfort hours (when you want higher temperature).

### Options

**`port`** (int, default: `8890`)
- HTTP server port for temperature API

**`rooms`** (map)
- Room configurations keyed by room ID

#### Room Configuration

**`name`** (string)
- Human-readable room name

**`comfort_temp`** (float)
- Temperature setpoint during comfort hours (°C)

**`eco_temp`** (float)
- Temperature setpoint outside comfort hours (°C)

**`schedule.weekday`** (array of strings)
- Comfort hours on weekdays (Mon-Fri)
- Format: `["HH:MM-HH:MM"]`
- Multiple ranges supported: `["06:00-08:00", "20:00-23:00"]`
- Empty array `[]` = always eco

**`schedule.weekend`** (array of strings)
- Comfort hours on weekends (Sat-Sun)
- Same format as weekday

## Usage Examples

### 1. Development (config file)

```yaml
# myhome.yaml
daemon:
  mqtt_broker: ""  # Use embedded broker
  enable_temperature_service: true
  
temperatures:
  port: 8890
  rooms:
    living-room:
      name: "Living Room"
      comfort_temp: 21.0
      eco_temp: 17.0
      schedule:
        weekday: ["06:00-23:00"]
        weekend: ["08:00-23:00"]
```

```bash
myhome daemon run
```

### 2. Production (config file + flags)

```yaml
# /etc/myhome/myhome.yaml
daemon:
  mqtt_broker: "mqtt://mqtt.local:1883"
  mqtt_timeout: 30s
  enable_temperature_service: true
  
temperatures:
  port: 8890
  rooms:
    # ... room configs
```

```bash
# Override specific settings with flags
myhome daemon run --mqtt-timeout 60s --proxy-port 8080
```

### 3. Account credentials via environment variables

Credentials can be passed via env vars without embedding them in the config file:

```bash
export MYHOME_BEEM_EMAIL="you@example.com"
export MYHOME_BEEM_PASSWORD="secret"

myhome daemon run
```

## Precedence Rules

When the same setting is specified in multiple places:

1. **Command-line flag** wins (if specified)
2. **Environment variable** wins (if flag not specified)
3. **Config file** wins (if neither flag nor env var specified)
4. **Default value** used (if nothing specified)

### Example

```yaml
# myhome.yaml
daemon:
  mqtt_timeout: 14s
```

```bash
myhome daemon run --mqtt-timeout 60s
```

**Result**: `mqtt_timeout = 60s` (flag wins)

```bash
myhome daemon run
```

**Result**: `mqtt_timeout = 14s` (from config file)

```bash
# No config file
myhome daemon run
```

**Result**: `mqtt_timeout = 14s` (default value)

## Duration Format

Durations use Go's duration format:

- `s` = seconds (e.g., `30s`)
- `m` = minutes (e.g., `5m`)
- `h` = hours (e.g., `2h`)
- Combined: `1h30m`, `2m30s`

## Validation

The daemon validates configuration on startup:

- Duration values must be valid Go durations
- Port numbers must be in range 1-65535
- Boolean values must be `true` or `false`
- Required fields (like room names) must be present

Invalid configuration will cause startup failure with a descriptive error message.

## Best Practices

### Development
- Use config file in current directory
- Enable all services for testing
- Use embedded broker

### Production
- Use `/etc/myhome/myhome.yaml`
- Specify external MQTT broker
- Use environment variables for secrets
- Override with flags for temporary changes

### Containers
- Use environment variables for credentials
- Mount config file for other settings
- Use secrets management for sensitive data

### Testing
- Use separate config files per environment
- Override with flags for quick tests
- Use `--events-dir` for debugging

## Troubleshooting

### Config file not found

```bash
myhome daemon run
# Output: No config file found, using defaults and flags
```

**Solution**: Create `myhome.yaml` in current directory or specify path.

### Config file found but ignored

Check precedence — flags and environment variables override config file.

```bash
# See what config file is loaded
myhome daemon run
# Output: Loaded config from: /path/to/myhome.yaml
```

### Service not starting

Check that service is enabled:

```yaml
daemon:
  enable_temperature_service: true
```

Or use flag:

```bash
myhome daemon run --enable-temperature-service
```

## Beem Energy

| Key | Default | Description |
|-----|---------|-------------|
| `beem.email` | — | Beem Energy account email (env: `MYHOME_BEEM_EMAIL`) |
| `beem.password` | — | Beem Energy account password (env: `MYHOME_BEEM_PASSWORD`) |
| `beem.poll_interval` | `60s` | How often to poll the Beem REST API |
| `beem.enabled` | `false` | Enable Beem Energy integration |

## Pool

The pool runtime tracker reports how many seconds the pool pump has run today by querying the shared events database (`events.db`). The gen2 listener already captures every switch ON/OFF event from all Shelly devices — no separate pool database is needed.

### Example

```yaml
pool:
  device_id: "aabbccddeeff"
  enabled: true
```

### Options

| Key | Flag | Default | Description |
|-----|------|---------|-------------|
| `pool.device_id` | `--pool-device-id` | — | Pool Shelly device ID (e.g. `shellyplus1pm-aabbccddeeff`) |
| `pool.enabled` | `--enable-pool` | `false` | Enable pool runtime tracking |

### Solar automation

The solar automation goroutine subscribes to Beem Energy power samples and controls the pool pump using a hysteresis state machine with four transitions:

- **IDLE → RUNNING** when `solar_w ≥ start_threshold_w` for `start_delay` AND daily runtime < hard ceiling
- **RUNNING → IDLE** when `solar_w < stop_threshold_w` for `stop_delay` (solar loss)
- **RUNNING → IDLE** when daily runtime ≥ `daily_target_sec` AND `solar_w < start_threshold_w` (soft stop: goal met, solar gone)
- **RUNNING → IDLE** when daily runtime ≥ `max_rotation_sec`, regardless of solar (hard ceiling)

Runtime targets (`daily_target_sec`, `max_rotation_sec`) are computed at startup from `min_volume_turnover` / `max_volume_turnover` multiplied by the pool volume and flow rate read from the pool device KVS. The daemon never writes to the device KVS.

Requires both `pool.device_id` and Beem Energy integration (`beem.enabled: true`) to be configured.

#### Example

```yaml
pool:
  device_id: "shellyplus1pm-aabbccddeeff"
  enabled: true
  solar:
    enabled: true
    start_threshold_w:   500
    stop_threshold_w:    200
    start_delay:         5m
    stop_delay:          10m
    min_volume_turnover: 5   # soft stop after 5× pool volumes filtered (if solar is gone)
    max_volume_turnover: 7   # hard ceiling at 7× pool volumes filtered
```

#### Options

| Key | Flag | Default | Description |
|-----|------|---------|-------------|
| `pool.solar.enabled` | `--enable-pool-solar` | `false` | Enable solar-driven pump automation |
| `pool.solar.start_threshold_w` | `--pool-solar-start-threshold-w` | `500` | Solar power threshold to start pump (W) |
| `pool.solar.stop_threshold_w` | `--pool-solar-stop-threshold-w` | `200` | Solar power threshold to stop pump (W) |
| `pool.solar.start_delay` | `--pool-solar-start-delay` | `5m` | Solar must hold above start threshold for this long |
| `pool.solar.stop_delay` | `--pool-solar-stop-delay` | `10m` | Solar must hold below stop threshold for this long |
| `pool.solar.min_volume_turnover` | `--pool-solar-min-volume-turnover` | `5` | Soft stop: pool volumes filtered per day after which the pump stops once solar is also gone (0 = no soft stop) |
| `pool.solar.max_volume_turnover` | `--pool-solar-max-volume-turnover` | `7` | Hard ceiling: always stop (and refuse to start) once this many pool volumes have been filtered, regardless of solar (0 = no ceiling; must be ≥ `min_volume_turnover` when both are set) |
