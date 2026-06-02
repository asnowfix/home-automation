# MyHome Configuration Guide

## Overview

MyHome uses a hierarchical configuration system with three levels of precedence:

1. **Command-line flags** (highest priority)
2. **Environment variables** (middle priority)
3. **Configuration file** (lowest priority)

This allows flexible deployment scenarios from development to production.

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
- Env: `MYHOME_DAEMON_MQTT_BROKER`

#### Timeouts and Intervals

**`mdns_timeout`** (duration, default: `7s`)
- Timeout for mDNS lookups
- Flag: `--mdns-timeout` or `-M`
- Env: `MYHOME_DAEMON_MDNS_TIMEOUT`

**`mqtt_timeout`** (duration, default: `14s`)
- Timeout for MQTT operations
- Flag: `--mqtt-timeout` or `-T`
- Env: `MYHOME_DAEMON_MQTT_TIMEOUT`

**`mqtt_grace`** (duration, default: `2s`)
- MQTT disconnection grace period
- Flag: `--mqtt-grace` or `-G`
- Env: `MYHOME_DAEMON_MQTT_GRACE`

**`refresh_interval`** (duration, default: `1m`)
- Known devices refresh interval
- Flag: `--refresh-interval` or `-R`
- Env: `MYHOME_DAEMON_REFRESH_INTERVAL`

**`mqtt_watchdog_interval`** (duration, default: `30s`)
- MQTT watchdog check interval
- Flag: `--mqtt-watchdog-interval` or `-W`
- Env: `MYHOME_DAEMON_MQTT_WATCHDOG_INTERVAL`

**`mqtt_watchdog_max_failures`** (int, default: `3`)
- Max consecutive failures before restart
- Flag: `--mqtt-watchdog-max-failures` or `-F`
- Env: `MYHOME_DAEMON_MQTT_WATCHDOG_MAX_FAILURES`

**`mqtt_reconnect_interval`** (duration, default: `2h`)
- Interval for periodic MQTT reconnection to refresh retained messages
- Useful after suspend/resume cycles to ensure latest device states
- Set to `0` to disable periodic reconnection
- Flag: `--mqtt-reconnect-interval`
- Env: `MYHOME_DAEMON_MQTT_RECONNECT_INTERVAL`

**`mqtt_broker_client_log_interval`** (duration, default: `2m`)
- Interval for logging MQTT broker connected clients
- Set to `0` to disable
- Flag: `--mqtt-broker-client-log-interval`
- Env: `MYHOME_DAEMON_MQTT_BROKER_CLIENT_LOG_INTERVAL`

#### Service Ports

**`ui_port`** (int, default: `6080`)
- UI listen port
- Flag: `--ui-port` or `-u`
- Env: `MYHOME_DAEMON_UI_PORT`

**`remote_proxy`** (string, default: `""`)
- Forward all `/devices/...` HTTP requests to a remote myhome daemon instead of connecting to devices directly. Useful when running a local myhome instance that reaches the home network via SSH port-forwarding and cannot dial device IPs directly.
- Example: `http://home-pi:6080` or `http://localhost:6081` (when `ssh -L 6081:localhost:6080 home-pi`)
- Flag: `--remote-proxy`
- Env: `MYHOME_DAEMON_REMOTE_PROXY`

#### Service Enablement

**`enable_gen1_proxy`** (bool, default: auto)
- Enable Gen1 HTTP->MQTT proxy
- Auto-enabled when using embedded broker
- Flag: `--enable-gen1-proxy` / `--disable-gen1-proxy`
- Env: `MYHOME_DAEMON_ENABLE_GEN1_PROXY`

**`enable_occupancy_service`** (bool, default: auto)
- Enable occupancy detection service (port 8889)
- Auto-enabled when using embedded broker
- Flag: `--enable-occupancy-service` / `--disable-occupancy-service`
- Env: `MYHOME_DAEMON_ENABLE_OCCUPANCY_SERVICE`

**`enable_temperature_service`** (bool, default: auto)
- Enable temperature scheduling service (port 8890)
- Auto-enabled when using embedded broker
- Requires `temperatures` section in config
- Flag: `--enable-temperature-service` / `--disable-temperature-service`
- Env: `MYHOME_DAEMON_ENABLE_TEMPERATURE_SERVICE`

#### Device Manager

**`disable_device_manager`** (bool, default: `false`)
- Disable the device manager
- Flag: `--disable-device-manager` or `-D`
- Env: `MYHOME_DAEMON_DISABLE_DEVICE_MANAGER`

#### Event Logging

**`events_dir`** (string, default: `""`)
- Directory to write received MQTT events as JSON files
- Empty = disabled
- Flag: `--events-dir` or `-E`
- Env: `MYHOME_DAEMON_EVENTS_DIR`

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
- Env: `MYHOME_EVENTS_DB`

**`retention`** (duration, default: `2160h`)
- How long events are kept before automatic deletion (90 days by default)
- Purge runs hourly; only the `events` table is purged (sensor daily stats are kept indefinitely)
- Set to `0` to disable automatic purging
- Flag: `--events-retention`
- Env: `MYHOME_EVENTS_RETENTION`

**`enabled`** (bool, default: `true`)
- Set to `false` to disable the event recording service entirely
- Flag: `--enable-events-service` / `--disable-events-service`
- Env: `MYHOME_EVENTS_ENABLED`

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

### 3. Container/Cloud (environment variables)

```bash
# No config file needed
export MYHOME_DAEMON_MQTT_BROKER="mqtt://mqtt.svc.cluster.local:1883"
export MYHOME_DAEMON_MQTT_TIMEOUT="30s"
export MYHOME_DAEMON_ENABLE_TEMPERATURE_SERVICE="true"
export MYHOME_TEMPERATURES_PORT="8890"

myhome daemon run
```

### 4. Hybrid (all three)

```yaml
# myhome.yaml - base configuration
daemon:
  mqtt_broker: "mqtt://mqtt.local:1883"
  mqtt_timeout: 14s
```

```bash
# Environment variable override
export MYHOME_DAEMON_MQTT_TIMEOUT="30s"

# Command-line flag override (highest priority)
myhome daemon run --mqtt-timeout 60s
# Result: mqtt_timeout = 60s (from flag)
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
export MYHOME_DAEMON_MQTT_TIMEOUT="30s"
myhome daemon run --mqtt-timeout 60s
```

**Result**: `mqtt_timeout = 60s` (flag wins)

```bash
export MYHOME_DAEMON_MQTT_TIMEOUT="30s"
myhome daemon run
```

**Result**: `mqtt_timeout = 30s` (env var wins over config file)

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

## Environment Variable Naming

Environment variables follow this pattern:

```
MYHOME_<SECTION>_<KEY>
```

Examples:
- `daemon.mqtt_broker` → `MYHOME_DAEMON_MQTT_BROKER`
- `daemon.mqtt_timeout` → `MYHOME_DAEMON_MQTT_TIMEOUT`
- `temperatures.port` → `MYHOME_TEMPERATURES_PORT`

**Note**: Nested keys use underscores, not dots.

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
- Use environment variables primarily
- Mount config file for complex settings
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

Check precedence - flags and environment variables override config file.

```bash
# See what config file is loaded
myhome daemon run
# Output: Loaded config from: /path/to/myhome.yaml
```

### Environment variables not working

Ensure correct naming:
- Prefix: `MYHOME_`
- Section: `DAEMON_` or `TEMPERATURES_`
- Key: uppercase with underscores

```bash
# Wrong
export MYHOME_MQTT_BROKER="..."

# Correct
export MYHOME_DAEMON_MQTT_BROKER="..."
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

| Key | Env var | Default | Description |
|-----|---------|---------|-------------|
| `beem.email` | `MYHOME_BEEM_EMAIL` | — | Beem Energy account email |
| `beem.password` | `MYHOME_BEEM_PASSWORD` | — | Beem Energy account password |
| `beem.poll_interval` | `MYHOME_BEEM_POLL_INTERVAL` | `60s` | How often to poll the Beem REST API |
| `beem.enabled` | `MYHOME_BEEM_ENABLED` | `false` | Enable Beem Energy integration |

## Pool

The pool runtime tracker reports how many seconds the pool pump has run today by querying the shared events database (`events.db`). The gen2 listener already captures every switch ON/OFF event from all Shelly devices — no separate pool database is needed.

### Example

```yaml
pool:
  device_id: "aabbccddeeff"
  enabled: true
```

### Options

| Key | Env var | Flag | Default | Description |
|-----|---------|------|---------|-------------|
| `pool.device_id` | `MYHOME_POOL_DEVICE_ID` | `--pool-device-id` | — | Pool Shelly device ID (e.g. `shellyplus1pm-aabbccddeeff`) |
| `pool.enabled` | `MYHOME_POOL_ENABLED` | `--enable-pool` | `false` | Enable pool runtime tracking |

### Solar automation

The solar automation goroutine subscribes to Beem Energy power samples and controls the pool pump using a hysteresis state machine:

- **IDLE → RUNNING** when `solar_w ≥ start_threshold_w` for `start_delay`
- **RUNNING → IDLE** when `solar_w < stop_threshold_w` for `stop_delay`
- **RUNNING → IDLE** when daily filtration target is reached (if `daily_target_sec > 0`)

Requires both `pool.device_id` and Beem Energy integration (`beem.enabled: true`) to be configured.

#### Example

```yaml
pool:
  device_id: "shellyplus1pm-aabbccddeeff"
  enabled: true
  solar:
    enabled: true
    start_threshold_w: 500
    stop_threshold_w:  200
    start_delay:       5m
    stop_delay:        10m
    daily_target_sec:  0   # 0 = run as long as solar is available
```

#### Options

| Key | Env var | Flag | Default | Description |
|-----|---------|------|---------|-------------|
| `pool.solar.enabled` | `MYHOME_POOL_SOLAR_ENABLED` | `--enable-pool-solar` | `false` | Enable solar-driven pump automation |
| `pool.solar.start_threshold_w` | `MYHOME_POOL_SOLAR_START_THRESHOLD_W` | `--pool-solar-start-threshold-w` | `500` | Solar power threshold to start pump (W) |
| `pool.solar.stop_threshold_w` | `MYHOME_POOL_SOLAR_STOP_THRESHOLD_W` | `--pool-solar-stop-threshold-w` | `200` | Solar power threshold to stop pump (W) |
| `pool.solar.start_delay` | `MYHOME_POOL_SOLAR_START_DELAY` | `--pool-solar-start-delay` | `5m` | Solar must hold above start threshold for this long |
| `pool.solar.stop_delay` | `MYHOME_POOL_SOLAR_STOP_DELAY` | `--pool-solar-stop-delay` | `10m` | Solar must hold below stop threshold for this long |
| `pool.solar.daily_target_sec` | `MYHOME_POOL_SOLAR_DAILY_TARGET_SEC` | `--pool-solar-daily-target-sec` | `0` | Daily filtration target in seconds; pump won't start via solar once reached (0 = no check) |
