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
  mqtt_broker_client_log_interval: 2m
  
  # Service Ports
  proxy_port: 6080
  
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

**`mqtt_broker_client_log_interval`** (duration, default: `2m`)
- Interval for logging MQTT broker connected clients
- Set to `0` to disable
- Flag: `--mqtt-broker-client-log-interval`
- Env: `MYHOME_DAEMON_MQTT_BROKER_CLIENT_LOG_INTERVAL`

#### Service Ports

**`proxy_port`** (int, default: `6080`)
- Reverse proxy listen port
- Flag: `--proxy-port` or `-p`
- Env: `MYHOME_DAEMON_PROXY_PORT`

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
