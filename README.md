# MyHome - Penates

## Abstract

MyHome Penates is the home automation system I develop & use to control my own house (and learn Go).  I use mostly (very cool) Shelly devices, from Alterco Robotics.

## Table of Contents <!-- omit in toc -->

- [MyHome - Penates](#myhome---penates)
  - [Abstract](#abstract)
  - [Design Philosophy](#design-philosophy)
  - [Releases](#releases)
  - [Development Tools](#development-tools)
    - [Shelly Device Data Collector](#shelly-device-data-collector)
  - [Logging System](#logging-system)
    - [Log Levels](#log-levels)
    - [Usage Examples](#usage-examples)
    - [Available Flags](#available-flags)
    - [VSCode Development](#vscode-development)
    - [Per-Package Logging](#per-package-logging)
    - [Environment Variables](#environment-variables)
  - [Usage - Linux](#usage---linux)
    - [Is daemon running?](#is-daemon-running)
    - [Manual start](#manual-start)
  - [Usage Windows](#usage-windows)
  - [Reporting issues](#reporting-issues)
    - [Issue Labels](#issue-labels)
      - [Standard GitHub Labels](#standard-github-labels)
      - [Project-Specific Labels](#project-specific-labels)
  - [Temperature Management](#temperature-management)
    - [Set Room Temperature Configuration](#set-room-temperature-configuration)
    - [Get Room Configuration](#get-room-configuration)
    - [List All Rooms](#list-all-rooms)
    - [Get Current Setpoint](#get-current-setpoint)
    - [Delete Room Configuration](#delete-room-configuration)
    - [Configure Heater to Use Room](#configure-heater-to-use-room)
    - [Service Auto-Enablement](#service-auto-enablement)
  - [Device Control](#device-control)
    - [Switch Command](#switch-command)
  - [Device Following](#device-following)
    - [Follow Shelly Device Status](#follow-shelly-device-status)
    - [Follow Shelly BLU Device](#follow-shelly-blu-device)
    - [How It Works](#how-it-works)
  - [Prometheus Metrics Exporter](#prometheus-metrics-exporter)
    - [Architecture](#architecture)
    - [Benefits](#benefits)
    - [Configuration](#configuration)
      - [Enable the Service](#enable-the-service)
      - [Shelly Device Setup](#shelly-device-setup)
      - [Prometheus Configuration](#prometheus-configuration)
    - [Available Metrics](#available-metrics)
    - [Testing](#testing)
    - [Troubleshooting](#troubleshooting)
  - [Heaters adaptative control](#heaters-adaptative-control)
    - [Kalman Filter Heater Control Script](#kalman-filter-heater-control-script)
  - [Shelly Notes](#shelly-notes)
    - [Shelly 1 H\&T](#shelly-1-ht)
    - [Web-Sockets Logs](#web-sockets-logs)
    - [Shelly MQTT Notes](#shelly-mqtt-notes)
      - [Any topic](#any-topic)
      - [Shelly H\&T Gen1 (FIXME)](#shelly-ht-gen1-fixme)
      - [Test MQTT CLI](#test-mqtt-cli)
      - [Shelly H\&T Gen1](#shelly-ht-gen1)
  - [GCP Notes](#gcp-notes)
  - [Shelly Devices](#shelly-devices)
    - [BLE - Bluetooth Low Energy](#ble---bluetooth-low-energy)
    - [Gen 3](#gen-3)
    - [Gen 2](#gen-2)
      - [Pro1 - Gen 2](#pro1---gen-2)
      - [Plus1 - Gen2](#plus1---gen2)
    - [Gen 1](#gen-1)
      - [HT - Gen1](#ht---gen1)
  - [Red-by-SFR Box Notes](#red-by-sfr-box-notes)
    - [Main API](#main-api)
    - [UPnP](#upnp)
    - [Port reserved by SFR-Box](#port-reserved-by-sfr-box)
  - [References](#references)

## Design Philosophy

MyHome is designed with the following core principles:

- **Cloud-Independent**: The system operates entirely on your local network without requiring cloud connectivity or external services.
- **Decentralized Architecture**: There is no central device manager or controller that maintains persistent state about your devices. The system relies entirely on discovering and interacting with devices that are currently present on the network.
- **Minimal Infrastructure**: The only required central component is an MQTT broker, which serves as a lightweight message bus for device communication.
- **Ephemeral Discovery**: Device management has no "stickiness" - devices are discovered dynamically when needed, and the system adapts to devices joining or leaving the network without maintaining a persistent registry.
- **Local Control**: All automation logic runs locally, ensuring your home continues to function even without internet access.

This architecture ensures resilience, privacy, and independence from third-party services while keeping the system simple and maintainable.  At the same time, it does not disable any device features that require cloud connectivity & hence can be used along with the device manufacturers application (such as Shelly app & Cloud Services).

## Releases

Published here: <https://github.com/asnowfix/home-automation/releases>.

## Development Tools

### Shelly Device Data Collector

A comprehensive tool for collecting API interaction data from all local Shelly devices for non-regression testing. The data collector systematically calls known API methods on discovered devices and records request/response pairs in JSON format.

**Location**: [`cmd/datacollector/`](cmd/datacollector/)

**Key Features**:
- Automatic device discovery via MQTT
- Comprehensive API method testing (17+ methods per device)
- Structured JSON output for test suite integration
- Error handling and timeout management
- Support for all Shelly device types (Gen1, Gen2, Gen3)

**Usage**:
```bash
cd cmd/datacollector
go build -o datacollector .
./datacollector
```

Results are saved to `test_data/shelly_api_test_data_YYYYMMDD_HHMMSS.json` for use in automated testing.

For detailed documentation, see the [Data Collector README](cmd/datacollector/README.md).

## Logging System

MyHome uses a flexible, per-package logging system with automatic environment detection for optimal development and production experience.

### Log Levels

The system supports standard log levels with different default behaviors based on the execution environment:

| **Environment** | **Default Level** | **Description** |
|-----------------|-------------------|-----------------|
| **Normal CLI** | `error` | Clean output for production use |
| **VSCode Debug** | `debug` | **Auto-detected**, full debugging info |
| **Daemon** | `warning` | Service-appropriate visibility |
| **Manual flags** | As specified | `--verbose`, `--debug`, `-L level` |

### Usage Examples

```bash
# Clean output (error level - default for CLI)
myhome ctl list

# Info level logging (verbose)
myhome ctl --verbose list
myhome ctl -v list

# Info level logging (explicit)
myhome ctl --log-level info list
myhome ctl -L info list

# Debug level logging
myhome ctl --debug list
myhome ctl --log-level debug list
myhome ctl -L debug list

# Specific log levels
myhome ctl -L warn list    # Warning level
myhome ctl -L error list   # Error level only
```

### Available Flags

- **`-v, --verbose`**: Verbose output (equivalent to `--log-level info`)
- **`--debug`**: Debug output (equivalent to `--log-level debug`)
- **`-L, --log-level`**: Set explicit log level (`error`, `warn`, `info`, `debug`)

### VSCode Development

When running processes through VSCode's debugger (launch.json), the logging system **automatically detects** the development environment and enables debug-level logging without any configuration changes. This provides rich debugging information during development while maintaining clean output for production use.

### Per-Package Logging

The system provides per-package loggers with context information. Each package gets its own named logger (e.g., `pkg/shelly/script`, `myhome/ctl/shelly`) for better log organization and debugging.

### Environment Variables

- **`MYHOME_LOG=stderr`**: Force logging to stderr (automatically set in VSCode launch configs)
- **`MYHOME_DEBUG_INIT=1`**: Show logging system initialization messages (for debugging the logger itself)

## Usage - Linux

### Is daemon running?

```shell
$ systemctl status myhome@fix.service
myhome@fix.service - MyHome as a system service
     Loaded: loaded (/etc/systemd/system/myhome@.service; enabled; vendor preset: enabled)
     Active: activating (auto-restart) (Result: exit-code) since Wed 2024-05-01 10:23:50 CEST; 1s ago
    Process: 3150933 ExecStart=/usr/bin/env /home/fix/go/bin/myhome -v (code=exited, status=127)
   Main PID: 3150933 (code=exited, status=127)
```

### Manual start

```bash
make start
```

## Usage Windows

Unless you suceed to set `$env:Path` in pwsh, you need to call GNU Make with its full Path.

```bash
C:\ProgramData\chocolatey\bin\make build
```

## Reporting issues

Please report issues on GitHub: <https://github.com/asnowfix/home-automation/issues>

### Issue Labels

When reporting issues, please use the appropriate label from the following categories:

#### Standard GitHub Labels

| Label | Description |
|-------|-------------|
| `bug` | Something isn't working |
| `documentation` | Improvements or additions to documentation |
| `duplicate` | This issue or pull request already exists |
| `enhancement` | New feature or request |
| `good first issue` | Good for newcomers |
| `help wanted` | Extra attention is needed |
| `invalid` | This doesn't seem right |
| `question` | Further information is requested |
| `wontfix` | This will not be worked on |

#### Project-Specific Labels

| Label | Description |
|-------|-------------|
| `license` | License-related tasks |
| `core-architecture` | Core architecture and design tasks |
| `user-interface` | User interface improvements and features |
| `integrations` | Integration with external systems and protocols |
| `code-quality` | Code quality improvements and refactoring |
| `packaging` | Packaging and deployment tasks |
| `device-feature` | Device-specific features and improvements |
| `monitoring` | Monitoring and metrics features |
| `networking` | Networking and device discovery features |

## Base Configuration

The `myhome ctl shelly setup` command setup a new device with common parameters.  It can also be used to refresh/cleanup existing configurations.

1. The **Matter** protocol is disabled (as it consumes too much Javascript resource)
2. MQTT broker address is set to the current MQTT broker in use by the configurating process.
2. NTP servers XXX
3. XXX
4. WiFi AP is disabled, **unless** there are connected clients
   1. In case clients are connected but the local device does not have an AP password, setup fails (the fix being to setup a password on both the WiFi AP & STA to avoid loss of connection).

## Temperature Management

The temperature service provides centralized temperature setpoint management for heater devices via MQTT RPC. Configurations are stored in SQLite and accessed via CLI commands.

### Set Room Temperature Configuration

Create or update a room's temperature settings with comfort/eco temperatures and schedules:

```shell
# Basic configuration
myhome ctl temperature set living-room \
  --name "Living Room" \
  --comfort 21 \
  --eco 17 \
  --weekday "06:00-23:00" \
  --weekend "08:00-23:00"

# Multiple time ranges (morning and evening comfort)
myhome ctl temperature set bedroom \
  --name "Bedroom" \
  --comfort 19 \
  --eco 16 \
  --weekday "06:00-08:00,20:00-23:00" \
  --weekend "08:00-23:00"

# Office (work hours only, always eco on weekends)
myhome ctl temperature set office \
  --name "Home Office" \
  --comfort 20 \
  --eco 17 \
  --weekday "08:00-18:00" \
  --weekend ""
```

**Schedule Philosophy**: Eco is the default - only define comfort hours. Any time not in the comfort schedule uses eco temperature.

### Get Room Configuration

```shell
myhome ctl temperature get living-room
```

**Output:**
```
Room: Living Room (living-room)
Comfort Temperature: 21.0°C
Eco Temperature: 17.0°C

Weekday Schedule (Comfort Hours):
  06:00 - 23:00

Weekend Schedule (Comfort Hours):
  08:00 - 23:00
```

### List All Rooms

```shell
myhome ctl temperature list
```

**Output:**
```
ROOM ID       NAME          COMFORT  ECO    WEEKDAY SCHEDULE    WEEKEND SCHEDULE
-------       ----          -------  ---    ----------------    ----------------
living-room   Living Room   21.0°C   17.0°C 06:00-23:00         08:00-23:00
bedroom       Bedroom       19.0°C   16.0°C 06:00-08:00, 20:00-23:00  08:00-23:00
office        Office        20.0°C   17.0°C 08:00-18:00         (always eco)
```

### Get Current Setpoint

Get the active temperature setpoint based on current time and schedule:

```shell
myhome ctl temperature setpoint living-room
```

**Output:**
```
Room: living-room
Current Time: 14:30

Active Setpoint: 21.0°C (comfort_hours)
Comfort Setpoint: 21.0°C
Eco Setpoint: 17.0°C
```

### Delete Room Configuration

```shell
myhome ctl temperature delete living-room
```

### Configure Heater to Use Room

Link a heater device to a room configuration:

```shell
# Set room ID in heater's KVS
myhome ctl shelly kvs set heater-living script/heater/room-id living-room
```

The heater script will automatically call `temperature.getsetpoint` via MQTT RPC to get the current target temperature.

### Service Auto-Enablement

**The temperature and occupancy services are automatically enabled when the device manager is running** (default behavior). You only need explicit flags if you want to:

- **Disable** a service: `--disable-temperature-service` or `--disable-occupancy-service`
- **Force enable** when device manager is disabled: `--enable-temperature-service` or `--enable-occupancy-service`

**Default behavior:**
```shell
# Both services auto-enabled
myhome daemon run

# Disable temperature service
myhome daemon run --disable-temperature-service

# Disable device manager (also disables temperature/occupancy)
myhome daemon run --disable-device-manager
```

**Configuration file:**
```yaml
daemon:
  # Optional - services auto-enable by default
  enable_temperature_service: true
  enable_occupancy_service: true
```

### Weather Forecast

Devices running **heater.js** will automatically fetch the weather forecast at startup & then every day for the 24 hours to come.

When Internet connectivity is down, neither initial fetch (eg. at script reboot) & nor refresh of the forecast work any longer.  The server has its own copy of the forecast, that devices can fetch using usual RPC using command `myhome.weather`.

## Proxy & Caching

### Cloud API's

The server implements the HTTP-over-MQTT `myhome.fetch` RPC verb that goes through a caching proxy to fetch public web resources & share them across devices.  Any subsequent request (from the same device or another one) to get the same resource within the cached duration specified by the server will returned the cached value.  If the remote resoruce is not available (server down or connectivity loss), the cached value will be returned instead.

The cache is persisted on server side, so server restart does not flushes it.

### MQTT replay

When devices restart, they need to wait for sensor-emitted event before having an up-to-date view of the entire system. The RPC verbs `mqtt.cache` and `mqtt.replay` bring the last known state of any given topic subscription.

## Graphical User Interface

## Device Control

`normally-closed` devices.

### Switch Command

The `switch` command provides subcommands to control device switches:

```shell
myhome ctl switch [toggle|on|off] <device-identifier>
```

**Subcommands:**
- `toggle`: Toggle the current switch state of the device (default `switch:0`)
- `on`: Turn the device switch on (default `switch:0`)
- `off`: Turn the device switch off (default `switch:0`)
- `status`:  return the partial (on/off) or full status of any given switch.  It can also return the partial status of every available switch on the device.

**Examples:**

Toggle a device (switches between on/off):
```shell
myhome ctl switch toggle lumiere-exterieure-droite
```

Turn a device on:
```shell
myhome ctl switch on lumiere-exterieure-droite
```

Turn a device off:
```shell
myhome ctl switch off lumiere-exterieure-droite
```

**Flags:**
- `-S, --switch int`: Use specific switch ID (default: 0)

**Help:**
```shell
myhome ctl switch --help              # Show available subcommands
myhome ctl switch toggle --help       # Help for toggle subcommand
myhome ctl switch on --help           # Help for on subcommand
myhome ctl switch off --help          # Help for off subcommand
```

## Device Following

The `follow` command allows you to configure devices to automatically respond to other devices or BLE devices. This creates automation relationships where one device (follower) reacts to changes in another device (followed).

### Follow Shelly Device Status

Configure a Shelly device to follow another Shelly device's status:

```shell
myhome ctl follow shelly <follower-device> <followed-device> [flags]
```

**Examples:**

Mirror a switch state (when followed device switch turns on/off, follower mirrors the action):
```shell
myhome ctl follow shelly mezzanine lustre --follow-id switch:0 --switch-id switch:0
```

Toggle on button press (when followed device input button is pressed, follower toggles):
```shell
myhome ctl follow shelly mezzanine mirroir-salon --follow-id input:0 --switch-id switch:0
```

**Flags:**
- `--follow-id`: Remote input ID to monitor (default: "switch:0")
  - `switch:X`: Mirror the given relay (switch) state
  - `input:X`: Toggle on button press (triggers on button release)
- `--switch-id`: Local switch ID to control (default: "switch:0")

### Follow Shelly BLU Device

Configure a Shelly device to follow a Shelly BLU (Bluetooth Low Energy) device:

```shell
myhome ctl follow blu <follower-device> <blu-mac> [flags]
```

**Example:**

Configure a device to turn on when BLU motion is detected:
```shell
myhome ctl follow blu mezzanine e8:e0:7e:d0:f9:89 --switch-id switch:0 --auto-off 300
```

**Flags:**
- `--switch-id`: Switch ID to operate (default: "switch:0")
- `--auto-off`: Seconds before auto turning off (default: 300)
- `--illuminance-min`: Minimum illuminance (lux) to trigger (default: 0)
- `--illuminance-max`: Maximum illuminance (lux) to trigger (default: 10)
- `--next-switch`: Optional next switch ID to turn on after auto-off

### How It Works

The follow commands configure the follower device with a script that:

1. **For Shelly devices**: Listens to MQTT events from the followed device and reacts based on the configured input type
2. **For BLU devices**: Monitors BLE advertisements and triggers actions based on motion detection and illuminance levels

The action is automatically inferred from the input type:
- **switch:X inputs**: Mirror the exact state (on/off)
- **input:X inputs**: Toggle the follower's switch when the button is released (`state: false`)

## Prometheus Metrics Exporter

The Prometheus Metrics Exporter is an integrated service that collects metrics from Shelly devices via MQTT and exposes them via HTTP for Prometheus scraping. This eliminates the need for HTTP endpoints on each device, saving ~4-5KB memory per device.

### Architecture

```
Shelly Device → MQTT Broker → MyHome Daemon → Prometheus
(watchdog.js)   (publish)     (metrics exporter)  (scrape)
```

### Benefits

- **Memory efficient**: Saves 4-5KB per Shelly device (no HTTP endpoint overhead)
- **Centralized**: One endpoint for all devices
- **mDNS support**: Can scrape from `myhome.local:9100`
- **NAT/firewall friendly**: Devices push to MQTT
- **Dynamic IP support**: Devices identified by ID, not IP

### Configuration

#### Enable the Service

The metrics exporter is **automatically enabled** when the device manager is running (default):

```bash
# Auto-enabled with device manager
myhome run

# Customize settings
myhome run --metrics-exporter-port 9100 --metrics-exporter-topic shelly/metrics

# Standalone (without device manager)
myhome run --disable-device-manager --enable-metrics-exporter --mqtt-broker tcp://broker:1883
```

#### Shelly Device Setup

Deploy the updated `watchdog.js` script to your devices:

```bash
myhome ctl shelly script update device-name watchdog.js
```

The script publishes metrics to MQTT every 30 seconds:

```javascript
prometheus: {
    enabled: true,
    publishIntervalSeconds: 30,
    mqttTopic: "shelly/metrics",
    monitoredSwitches: ["switch:0"]
}
```

#### Prometheus Configuration

```yaml
scrape_configs:
  - job_name: 'shelly'
    scrape_interval: 30s
    static_configs:
      - targets: ['myhome.local:9100']  # If mDNS supported
      # OR
      - targets: ['192.168.1.2:9100']   # IP address
```

### Available Metrics

**System Metrics:**
- `shelly_uptime_seconds` (counter) - System uptime
- `shelly_ram_size_bytes` (gauge) - Total RAM
- `shelly_ram_free_bytes` (gauge) - Free RAM

**Switch Metrics** (per monitored switch):
- `shelly_switch_power_watts` (gauge) - Power consumption
- `shelly_switch_voltage_volts` (gauge) - Voltage
- `shelly_switch_current_amperes` (gauge) - Current
- `shelly_switch_temperature_celsius` (gauge) - Temperature
- `shelly_switch_power_total` (counter) - Total energy consumed
- `shelly_switch_output` (gauge) - Switch state (1=on, 0=off)

All metrics include labels: `name`, `id`, `mac`, `app`, `switch`

### Testing

```bash
# View metrics
curl http://localhost:9100/metrics

# Check health
curl http://localhost:9100/health

# Monitor MQTT messages
mosquitto_sub -h mqtt-broker -t 'shelly/metrics/#' -v
```

### Troubleshooting

**Metrics not appearing:**
1. Check MQTT: `mosquitto_sub -h broker -t 'shelly/metrics/#' -v`
2. Check device script: `myhome ctl shelly script status device-name watchdog.js`
3. Verify MQTT connection on device

**Port already in use:**
```bash
lsof -i :9100
# Use different port
myhome run --metrics-exporter-port 9101
```

For detailed documentation, see [`docs/metrics-exporter.md`](docs/metrics-exporter.md).

## Heaters adaptative control

The following object is to be stored in the KV store, with key `script/heater/config`:

```json
{
  "internal_topic": "shellies/shellyht-ABC123/sensor/temperature",
  "external_topic": "shellyplus1pm-XYZ789/events/rpc",
  "setpoint": 20.5,
  "min_temp": 14.0,
  "cheap_start": 22,
  "cheap_end": 6,
  "preheat_hours": 2,
  "poll_interval_ms": 300000,
  "accuweather_api_key": "YOUR_KEY",
  "accuweather_location_key": "YOUR_LOC_KEY",
  "meteofrance_api_key": "YOUR_MF_KEY",
  "meteofrance_lat": "48.8566",
  "meteofrance_lon": "2.3522"
}
```

**Temperature Source Configuration:**

The script now uses MQTT topics instead of HTTP URLs for temperature sources. It automatically detects the format:

- **Gen1 format**: `shellies/<device-id>/sensor/temperature`
  - Payload: Plain number (e.g., `20.5`)
  - Example: `shellies/shellyht-ABC123/sensor/temperature`

- **Gen2 format**: `<device-id>/events/rpc`
  - Payload: JSON with `NotifyStatus` method containing temperature components
  - Example: `shellyplus1pm-XYZ789/events/rpc`
  - Looks for `temperature:0`, `temperature:1`, or `temperature:2` in params

Temperature values are stored in the device's KVS under:
- `script/heater/internal` - Internal temperature
- `script/heater/external` - External temperature

### Kalman Filter Heater Control Script

The `heater.js` script is a Shelly script that uses a Kalman filter to control a heater.
It is designed to be used with a Shelly 1 Plus device connected to a relay that controls the heater.

**How it works:**
- Subscribes to MQTT topics for internal and external temperature sensors (supports both Gen1 and Gen2 Shelly devices)
- Stores received temperatures in the device's KVS for reliable access
- Uses a Kalman filter to estimate the current temperature of the room
- Controls the heater based on the filtered temperature vs. setpoint
- Uses AccuWeather and MeteoFrance APIs to get weather forecasts
- Implements pre-heating logic to reach setpoint by end of cheap electricity window
- Only heats during cheap electricity hours (`cheap_start` to `cheap_end`) when occupants are present

Home occupancy is detected by polling the occupancy sensor at the specified URL.

## Shelly Notes

```
http://192.168.33.1/rpc/HTTP.GET?url="http://admin:supersecretpassword@10.33.53.21/rpc/Shelly.Reboot"
```

### Shelly 1 H&T

URL update to sensor API:

```
févr. 15 22:04:09 palmbeach env[191666]: 2024/02/15 22:04:09 url: /?hum=89&temp=9.88&id=shellyht-EE45E9
févr. 15 22:04:09 palmbeach env[191666]: 2024/02/15 22:04:09 header: Content-Length: [0]
févr. 15 22:04:09 palmbeach env[191666]: 2024/02/15 22:04:09 header: User-Agent: [Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)]
févr. 15 22:04:09 palmbeach env[191666]: 2024/02/15 22:04:09 body:
```

Same as:

```
$ curl -X POST -H 'User-Agent: [Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)]' 'http://192.168.1.2:8888/?hum=89&temp=9.88&id=shellyht-
EE45E9'
```

Test output

```
go install
sudo systemctl stop myhome@fix.service
sudo systemctl start myhome@fix.service
systemctl status myhome@fix.service
```

### Web-Sockets Logs

From <https://shelly-api-docs.shelly.cloud/gen2/Scripts/Tutorial>:


```bash
export SHELLY=192.168.1.39
curl -X POST -d '{"id":1, "method":"Sys.SetConfig","params":{"config":{"debug":{"websocket":{"enable":true}}}}}' http://${SHELLY}/rpc
wscat --connect ws://${SHELLY}/debug/log
```
```log
< {"ts":1733774548.629, "level":2, "data":"shelly_debug.cpp:236    Streaming logs to 192.168.1.2:40234", "fd":1}
< {"ts":1733774573.492, "level":2, "data":"    \"component\": \"input:0\", ", "fd":102}
< {"ts":1733774573.494, "level":2, "data":"    \"id\": 0, ", "fd":102}
< {"ts":1733774573.497, "level":2, "data":"    \"event\": \"toggle\", ", "fd":102}
< {"ts":1733774573.499, "level":2, "data":"    \"state\": true, \"ts\": 1733774573.41000008583 }", "fd":102}
< {"ts":1733774573.501, "level":2, "data":" }", "fd":102}
< {"ts":1733774573.503, "level":2, "data":"Toggle lustre light", "fd":102}
< {"ts":1733774573.505, "level":2, "data":"shelly_ejs_rpc.cpp:41   Shelly.call HTTP.POST {\"url\":\"http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle\",\"body\":\"{\\\"id\\\":0}\"}", "fd":1}
< {"ts":1733774573.508, "level":2, "data":"shelly_notification:162 Status change of input:0: {\"id\":0,\"state\":true}", "fd":1}
< {"ts":1733774573.543, "level":2, "data":"shos_rpc_inst.c:243     HTTP.POST via loopback ", "fd":1}
< {"ts":1733774573.547, "level":2, "data":"shelly_http_client.:308 0x3ffe4998: HTTP POST http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle", "fd":1}
< {"ts":1733774573.670, "level":2, "data":"  \"id\": 0, \"now\": 1733774573.60793089866, ", "fd":102}
< {"ts":1733774573.672, "level":2, "data":"  \"info\": { ", "fd":102}
< {"ts":1733774573.674, "level":2, "data":"    \"component\": \"input:0\", ", "fd":102}
< {"ts":1733774573.676, "level":2, "data":"    \"id\": 0, ", "fd":102}
< {"ts":1733774573.678, "level":2, "data":"    \"event\": \"toggle\", ", "fd":102}
< {"ts":1733774573.680, "level":2, "data":"    \"state\": false, \"ts\": 1733774573.60999989509 }", "fd":102}
< {"ts":1733774573.682, "level":2, "data":" }", "fd":102}
< {"ts":1733774573.684, "level":2, "data":"shelly_notification:162 Status change of input:0: {\"id\":0,\"state\":false}", "fd":1}
< {"ts":1733774573.751, "level":2, "data":"shelly_http_client.:611 0x3ffe4998: Finished; bytes 132, code 200, redir 0/3, auth 0, status OK", "fd":1}
< {"ts":1733774574.909, "level":2, "data":"    \"component\": \"input:0\", ", "fd":102}
< {"ts":1733774574.912, "level":2, "data":"    \"id\": 0, ", "fd":102}
< {"ts":1733774574.913, "level":2, "data":"    \"event\": \"toggle\", ", "fd":102}
< {"ts":1733774574.915, "level":2, "data":"    \"state\": true, \"ts\": 1733774574.82999992370 }", "fd":102}
< {"ts":1733774574.917, "level":2, "data":" }", "fd":102}
< {"ts":1733774574.919, "level":2, "data":"Toggle lustre light", "fd":102}
< {"ts":1733774574.921, "level":2, "data":"shelly_ejs_rpc.cpp:41   Shelly.call HTTP.POST {\"url\":\"http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle\",\"body\":\"{\\\"id\\\":0}\"}", "fd":1}
< {"ts":1733774574.925, "level":2, "data":"shelly_notification:162 Status change of input:0: {\"id\":0,\"state\":true}", "fd":1}
< {"ts":1733774574.973, "level":2, "data":"shos_rpc_inst.c:243     HTTP.POST via loopback ", "fd":1}
< {"ts":1733774574.977, "level":2, "data":"shelly_http_client.:308 0x3ffe4a0c: HTTP POST http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle", "fd":1}
< {"ts":1733774574.980, "level":2, "data":"shos_init.c:94          New min heap free: 107092", "fd":1}
< {"ts":1733774574.982, "level":2, "data":"shos_init.c:94          New min heap free: 106164", "fd":1}
< {"ts":1733774574.996, "level":2, "data":"shos_init.c:94          New min heap free: 105400", "fd":1}
< {"ts":1733774575.020, "level":2, "data":"shelly_http_client.:611 0x3ffe4a0c: Finished; bytes 131, code 200, redir 0/3, auth 0, status OK", "fd":1}
< {"ts":1733774575.109, "level":2, "data":"  \"id\": 0, \"now\": 1733774575.04737496376, ", "fd":102}
< {"ts":1733774575.112, "level":2, "data":"  \"info\": { ", "fd":102}
< {"ts":1733774575.114, "level":2, "data":"    \"component\": \"input:0\", ", "fd":102}
< {"ts":1733774575.116, "level":2, "data":"    \"id\": 0, ", "fd":102}
< {"ts":1733774575.117, "level":2, "data":"    \"event\": \"toggle\", ", "fd":102}
< {"ts":1733774575.119, "level":2, "data":"    \"state\": false, \"ts\": 1733774575.04999995231 }", "fd":102}
< {"ts":1733774575.121, "level":2, "data":" }", "fd":102}
< {"ts":1733774575.123, "level":2, "data":"shelly_notification:162 Status change of input:0: {\"id\":0,\"state\":false}", "fd":1}
< {"ts":1733774575.144, "level":2, "data":"shos_init.c:94          New min heap free: 104656", "fd":1}
< {"ts":1733774584.700, "level":2, "data":"shelly_debug.cpp:149    Stopped streaming logs to 192.168.1.57:53127", "fd":1}
```

### Shelly MQTT Notes

- [Hive MQTT CLI Installation](https://hivemq.github.io/mqtt-cli/docs/installation/)

#### Any topic

```bash
mqtt sub -d -t '#' -h 192.168.1.2
```
```log
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=ctblbp0vpopou2bqq0t0, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' sending SUBSCRIBE
    MqttSubscribe{subscriptions=[MqttSubscription{topicFilter=#, qos=EXACTLY_ONCE, noLocal=false, retainHandling=SEND, retainAsPublished=false}]}
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received SUBACK
    MqttSubAck{reasonCodes=[GRANTED_QOS_2], packetIdentifier=65526}
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('true')
    MqttPublish{topic=shelly1minig3-54320464f17c/online, payload=4byte, qos=AT_LEAST_ONCE, retain=true}
true
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733776884.28,"input:3":{"id":3,"state":true}}}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/events/rpc, payload=162byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733776884.28,"input:3":{"id":3,"state":true}}}
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"id":3,"state":false}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:3, payload=22byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":3,"state":false}
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733776888.34,"input:3":{"id":3,"state":true}}}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/events/rpc, payload=162byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733776888.34,"input:3":{"id":3,"state":true}}}
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"src":"shelly1minig3-54320464f17c","dst":"shelly1minig3-54320464f17c/events","method":"NotifyStatus","params":{"ts":1733776888.48,"switch:0":{"id":0,"output":false,"source":"HTTP_in"}}}')
    MqttPublish{topic=shelly1minig3-54320464f17c/events/rpc, payload=186byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shelly1minig3-54320464f17c","dst":"shelly1minig3-54320464f17c/events","method":"NotifyStatus","params":{"ts":1733776888.48,"switch:0":{"id":0,"output":false,"source":"HTTP_in"}}}
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"id":3,"state":false}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:3, payload=22byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":3,"state":false}
[...]
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('shellyplusi4-c4d8d554ad6c 427 1733777582.752 1|shos_dns_sd_respond:236 ws(0x3ffde77c): Announced ShellyPlusI4-C4D8D554AD6C any@any (192.168.1.39)')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/debug/log, payload=145byte, qos=AT_MOST_ONCE, retain=false, messageExpiryInterval=86400}
[...]
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"id":0,"state":false}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:0, payload=22byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":0,"state":false}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=57}
```

Click & Release Button 2

```log
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733777785.79,"input:2":{"id":2,"state":true}}}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/events/rpc, payload=162byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733777785.79,"input:2":{"id":2,"state":true}}}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=58}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"id":2,"state":true}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:2, payload=21byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":2,"state":true}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=59}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733777785.98,"input:2":{"id":2,"state":false}}}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/events/rpc, payload=163byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733777785.98,"input:2":{"id":2,"state":false}}}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=60}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"id":2,"state":false}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:2, payload=22byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":2,"state":false}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=61}
```

#### Shelly H&T Gen1 (FIXME)

Debug log:

```log
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:23 > header: %s: %s Content-Length=["0"] v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:23 > header: %s: %s User-Agent=["Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)"] v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:44 > http.HandleFunc url=/?hum=69&temp=17.62&id=shellyht-208500 v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:46 > http.HandleFunc query={"hum":["69"],"id":["shellyht-208500"],"temp":["17.62"]} v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:68 > http.HandleFunc gen1_device={"humidity":69,"ip":"192.168.1.37"} v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:72 > http.HandleFunc gen1_json="{\"ip\":\"192.168.1.37\",\"humidity\":69}" v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/pkg/shelly/gen1/publisher.go:36 > gen1.Publisher: MQTT(%v) <<< %v shellyht-208500/events/rpc="{\"id\":0,\"tC\":17.62,\"tF\":63.716003}" v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/logs/waiter.go:13 > logs.Waiter: topic=shellyht-208500/events/rpc v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/logs/waiter.go:29 > logs.Waiter: already known topic=shellyht-208500/events/rpc v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/mymqtt/mqtt.go:211 > MqttSubscribe received: payload="{\"id\":0,\"tC\":17.62,\"tF\":63.716003}" topic=shellyht-208500/events/rpc v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/logs/waiter.go:25 > logs.Waiter payload="{\"id\":0,\"tC\":17.62,\"tF\":63.716003}" topic=shellyht-208500/events/rpc v=0
```

MQTT log:

```log
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"id":0, "source":"HTTP_in", "output":false,"temperature":{"tC":40.5, "tF":104.9}}')
    MqttPublish{topic=shelly1minig3-54320464f17c/status/switch:0, payload=82byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":0, "source":"HTTP_in", "output":false,"temperature":{"tC":40.5, "tF":104.9}}
```

#### Test MQTT CLI

```bash
mqtt sub -d -t shellyplusi4-c4d8d554ad6c/status/3 -h 192.168.1.2
```
```log
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=ctbl8t0vpopou2bqq0r0, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'ctbl8t0vpopou2bqq0r0@192.168.1.2' sending SUBSCRIBE
    MqttSubscribe{subscriptions=[MqttSubscription{topicFilter=shellyplusi4-c4d8d554ad6c/status/3, qos=EXACTLY_ONCE, noLocal=false, retainHandling=SEND, retainAsPublished=false}]}
Client 'ctbl8t0vpopou2bqq0r0@192.168.1.2' received SUBACK
    MqttSubAck{reasonCodes=[GRANTED_QOS_2], packetIdentifier=65526}
[...]
Client 'ctbl8t0vpopou2bqq0r0@192.168.1.2' received PUBLISH ('bar')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/3, payload=3byte, qos=AT_MOST_ONCE, retain=false, messageExpiryInterval=86400}
bar
```

```bash
mqtt pub --topic=shellyplusi4-c4d8d554ad6c/status/3 -m="bar" --host=192.168.1.2 --debug
```
```log
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=ctbla00vpopou2bqq0sg, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'ctbla00vpopou2bqq0sg@192.168.1.2' sending PUBLISH ('bar')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/3, payload=3byte, qos=AT_MOST_ONCE, retain=false}
Client 'ctbla00vpopou2bqq0sg@192.168.1.2' finish PUBLISH
    MqttPublishResult{publish=MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/3, payload=3byte, qos=AT_MOST_ONCE, retain=false}}
```


#### Shelly H&T Gen1

Subscribe to Shelly H&T Gen1:

```log
$ mqtt sub -d -t shellyht-EE45E9/events/rpc -h 192.168.1.2
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=cnfrgl0vpopiu8vsbo1g, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'cnfrgl0vpopiu8vsbo1g@192.168.1.2' sending SUBSCRIBE
    MqttSubscribe{subscriptions=[MqttSubscription{topicFilter=shellyht-EE45E9/events/rpc, qos=EXACTLY_ONCE, noLocal=false, retainHandling=SEND, retainAsPublished=false}]}
Client 'cnfrgl0vpopiu8vsbo1g@192.168.1.2' received SUBACK
    MqttSubAck{reasonCodes=[GRANTED_QOS_2], packetIdentifier=65526}
```

```log
$ mqtt pub --topic=foo -m="bar" --host=192.168.1.2 --debug
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=csoekd0vpoph78legnfg, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'csoekd0vpoph78legnfg@192.168.1.2' sending PUBLISH ('bar')
    MqttPublish{topic=foo, payload=3byte, qos=AT_MOST_ONCE, retain=false}
Client 'csoekd0vpoph78legnfg@192.168.1.2' finish PUBLISH
    MqttPublishResult{publish=MqttPublish{topic=foo, payload=3byte, qos=AT_MOST_ONCE, retain=false}}
```

Publish to Shelly H&T Gen1:

```shell
$ mqtt pub -d -t shellyht-EE45E9/events/rpc -h 192.168.1.2 -m '{"a":"b"}'
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=cngepjovpopiu8vsbo20, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'cngepjovpopiu8vsbo20@192.168.1.2' sending PUBLISH ('{"a":"b"}')
    MqttPublish{topic=shellyht-EE45E9/events/rpc, payload=9byte, qos=AT_MOST_ONCE, retain=false}
Client 'cngepjovpopiu8vsbo20@192.168.1.2' finish PUBLISH
    MqttPublishResult{publish=MqttPublish{topic=shellyht-EE45E9/events/rpc, payload=9byte, qos=AT_MOST_ONCE, retain=false}}
```

## GCP Notes

```shell
$ gcloud compute project-info describe --project "homeautomation-402816"
commonInstanceMetadata:
  fingerprint: dZXOiHlTSW8=
  kind: compute#metadata
creationTimestamp: '2023-11-01T02:10:02.993-07:00'
defaultNetworkTier: PREMIUM
defaultServiceAccount: 313423816598-compute@developer.gserviceaccount.com
id: '4099453077804788485'
kind: compute#project
name: homeautomation-402816
quotas:
- limit: 1000.0
  metric: SNAPSHOTS
  usage: 0.0
[...]
```

```shell
cd myzone
go run .
tonnara:myzone fix$ go run .
panic: googleapi: got HTTP response code 404 with body: <!DOCTYPE html>
<html lang=en>
  <meta charset=utf-8>
  <meta name=viewport content="initial-scale=1, minimum-scale=1, width=device-width">
  <title>Error 404 (Not Found)!!1</title>
  <style>
   [...]
  </style>
  <a href=//www.google.com/><span id=logo aria-label=Google></span></a>
  <p><b>404.</b> <ins>That’s an error.</ins>
  <p>The requested URL <code>/dns/v2/projects/homeautomation-402816/locations/europe-west9/managedZones</code> was not found on this server.  <ins>That’s all we know.</ins>
```

See <https://cloud.google.com/sdk/gcloud/reference/dns/managed-zones/list>

```shell
$ go run .
panic: googleapi: Error 401: API keys are not supported by this API. Expected OAuth2 access token or other authentication credentials that assert a principal. See https://cloud.google.com/docs/authentication
Details:
[
  {
    "@type": "type.googleapis.com/google.rpc.ErrorInfo",
    "domain": "googleapis.com",
    "metadata": {
      "method": "cloud.dns.api.v2.ManagedZonesService.List",
      "service": "dns.googleapis.com"
    },
    "reason": "CREDENTIALS_MISSING"
  }
]

More details:
Reason: required, Message: Login Required.
```

## Shelly Devices

### BLE - Bluetooth Low Energy

```json
[
  "e8:e0:7e:d0:f9:89",  // motion-front-door
  "b0:c7:de:11:58:d5",  // motion-parking
  "e8:e0:7e:a6:0c:6f",  // motion-pool-house
]
```

### Gen 3

### Gen 2

#### Pro1 - Gen 2

```json
{
  "model": "ShellyPro1",
  "mac": "30C6F782D274",
  "app": "Pro1",
  "ver": "1.0.8",
  "gen": 2,  "service": "shellypro1-30c6f782d274._shelly._tcp.local.",
  "host": "ShellyPro1-30C6F782D274.local.",
  "ipv4": "192.168.1.60",
  // ...
}
```

#### Plus1 - Gen2

```json
{
  "model": "ShellyPlus1",
  "mac": "08B61FD141E8",
  "app": "Plus1",
  "ver": "1.0.8",
  "gen": 2,
  "service": "shellyplus1-08b61fd141e8._shelly._tcp.local.",
  "host": "ShellyPlus1-08B61FD141E8.local.",
  "ipv4": "192.168.1.76",
  "port": 80,
  // ...
}
```

```shell
$ curl -s http://ShellyPlus1-4855199C9888.local/rpc/Switch.GetStatus?id=0 | jq
{
  "id": 0,
  "source": "init",
  "output": true,
  "temperature": {
    "tC": 52.4,
    "tF": 126.3
  }
}
```

```shell
$ curl -s http://ShellyPlus1-4855199C9888.local/rpc/Switch.GetConfig?id=0 | jq
{
  "id": 0,
  "name": "Development",
  "in_mode": "follow",
  "initial_state": "on",
  "auto_on": false,
  "auto_on_delay": 60,
  "auto_off": false,
  "auto_off_delay": 1
}
```

### Gen 1

#### HT - Gen1

MQTT Logs

```log
Client 'd3sjjq2k604lkbcvusg0@192.168.1.2' received PUBLISH ('20.25')
    MqttPublish{topic=shellies/shellyht-EE45E9/sensor/temperature, payload=5byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
20.25
Client 'd3sjjq2k604lkbcvusg0@192.168.1.2' received PUBLISH ('72')
    MqttPublish{topic=shellies/shellyht-EE45E9/sensor/humidity, payload=2byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
72
Client 'd3sjjq2k604lkbcvusg0@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=7}
Client 'd3sjjq2k604lkbcvusg0@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=8}
Client 'd3sjjq2k604lkbcvusg0@192.168.1.2' received PUBLISH ('22.75')
    MqttPublish{topic=shellies/shellyht-208500/sensor/temperature, payload=5byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
22.75
Client 'd3sjjq2k604lkbcvusg0@192.168.1.2' received PUBLISH ('62')
    MqttPublish{topic=shellies/shellyht-208500/sensor/humidity, payload=2byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
62
```

## Red-by-SFR Box Notes

### Main API

```shell
$ curl -s -G  http://192.168.1.1/api/1.0/?method=auth.getToken
<?xml version="1.0" encoding="UTF-8"?>
<rsp stat="ok" version="1.0">
     <auth token="665ae99c7ff692d186fdca08ba2a8c" method="all" />
</rsp>
```

### UPnP

```shell
$ sudo apt install xmlstarlet gupnp-tools
$ cat /proc/net/route | awk '{if($2=="00000000"){print $1}else{next}}'
enp1s0
$ gssdp-discover -i enp1s0 --timeout=3
[...]
resource available
  USN:      uuid:a6863339-b260-4d65-a9ac-6b73204d56f4::urn:neufboxtv-org:service:Resources:1
  Location: http://192.168.1.28:49153/uuid:7caa1f0b-ea52-485a-bd1d-5fe9ff0da2df/description.xml
[...]
resource available
  USN:      uuid:a04bed62-57f7-4885-91cc-e44e321a3ca7::urn:schemas-upnp-org:device:WANConnectionDevice:1
  Location: http://192.168.1.1:49152/rootDesc.xml
[...]
$ curl http://192.168.1.1:49152/rootDesc.xml | xmlstarlet fo
```
```xml
<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion>
    <major>1</major>
    <minor>0</minor>
  </specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
    <friendlyName>neufbox router</friendlyName>
    <manufacturer>neufbox</manufacturer>
    <manufacturerURL>http://efixo.com</manufacturerURL>
    <modelDescription>neufbox router</modelDescription>
    <modelName>neufbox router</modelName>
    <modelNumber>1</modelNumber>
    <modelURL>http://efixo.com</modelURL>
    <serialNumber>00000000</serialNumber>
    <UDN>uuid:a04bed62-57f7-4885-91cc-e44e321a3ca5</UDN>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:Layer3Forwarding:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:Layer3Forwarding1</serviceId>
        <controlURL>/ctl/L3F</controlURL>
        <eventSubURL>/evt/L3F</eventSubURL>
        <SCPDURL>/L3F.xml</SCPDURL>
      </service>
    </serviceList>
    <deviceList>
      <device>
        <deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>
        <friendlyName>WANDevice</friendlyName>
        <manufacturer>MiniUPnP</manufacturer>
        <manufacturerURL>http://miniupnp.free.fr/</manufacturerURL>
        <modelDescription>WAN Device</modelDescription>
        <modelName>WAN Device</modelName>
        <modelNumber>20220123</modelNumber>
        <modelURL>http://miniupnp.free.fr/</modelURL>
        <serialNumber>00000000</serialNumber>
        <UDN>uuid:a04bed62-57f7-4885-91cc-e44e321a3ca6</UDN>
        <UPC>000000000000</UPC>
        <serviceList>
          <service>
            <serviceType>urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1</serviceType>
            <serviceId>urn:upnp-org:serviceId:WANCommonIFC1</serviceId>
            <controlURL>/ctl/CmnIfCfg</controlURL>
            <eventSubURL>/evt/CmnIfCfg</eventSubURL>
            <SCPDURL>/WANCfg.xml</SCPDURL>
          </service>
        </serviceList>
        <deviceList>
          <device>
            <deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>
            <friendlyName>WANConnectionDevice</friendlyName>
            <manufacturer>MiniUPnP</manufacturer>
            <manufacturerURL>http://miniupnp.free.fr/</manufacturerURL>
            <modelDescription>MiniUPnP daemon</modelDescription>
            <modelName>MiniUPnPd</modelName>
            <modelNumber>20220123</modelNumber>
            <modelURL>http://miniupnp.free.fr/</modelURL>
            <serialNumber>00000000</serialNumber>
            <UDN>uuid:a04bed62-57f7-4885-91cc-e44e321a3ca7</UDN>
            <UPC>000000000000</UPC>
            <serviceList>
              <service>
                <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
                <serviceId>urn:upnp-org:serviceId:WANIPConn1</serviceId>
                <controlURL>/ctl/IPConn</controlURL>
                <eventSubURL>/evt/IPConn</eventSubURL>
                <SCPDURL>/WANIPCn.xml</SCPDURL>
              </service>
            </serviceList>
          </device>
        </deviceList>
      </device>
      <device>
        <deviceType>urn:schemas-upnp-org:device:EventDevice:1</deviceType>
        <friendlyName>NeufboxEventDevice</friendlyName>
        <manufacturer>efixo</manufacturer>
        <manufacturerURL>http://www.efixo.com/</manufacturerURL>
        <modelDescription>software Event Device</modelDescription>
        <modelName>Neufbox Event Device</modelName>
        <modelNumber>20220123</modelNumber>
        <modelURL>http://www.efixo.com/</modelURL>
        <serialNumber>00000000</serialNumber>
        <UDN>uuid:a04bed62-57f7-4885-91cc-e44e321a3ca8</UDN>
        <UPC>000000000000</UPC>
        <serviceList>
          <service>
            <serviceType>urn:neufbox-org:service:NeufBoxEvent:1</serviceType>
            <serviceId>urn:neufbox-org:serviceId:NeufBoxEvent1</serviceId>
            <controlURL>/ctl/NBX</controlURL>
            <eventSubURL>/evt/NBX</eventSubURL>
            <SCPDURL>/NBX.xml</SCPDURL>
          </service>
        </serviceList>
      </device>
    </deviceList>
    <presentationURL>http://192.168.1.1/</presentationURL>
  </device>
</root>```

## Mochi-MQTT Notes

```shell
$ go get github.com/mochi-mqtt/server/v2
```

### Port reserved by SFR-Box

These ports are not usable for NAT > Port Redirection.

```
1287/tcp
1288/tcp
1290-1339/tcp
2427/udp
5060/both
35500-35599/udp
68/udp
8254/udp
64035-65535/both
9/udp
5086/tcp
15086/udp
```

## References

1. Google Cloud
   1. <https://cloud.google.com/dns?hl=en>
   2. <https://cloud.google.com/dns/docs/registrars>
   3. <https://cloud.google.com/api-gateway/docs/reference/rest/v1/projects.locations>
   4. <https://pkg.go.dev/google.golang.org/api>
   5. <https://dcc.godaddy.com/manage/asnowfix.fr/dns>
   6. <https://console.cloud.google.com/net-services/dns/zones/asnowfix-root/details?project=homeautomation-402816>
   7. <https://console.cloud.google.com/home/dashboard?project=homeautomation-402816>
   8. <https://cloud.google.com/dns/docs/zones>
   9. <https://cloud.google.com/dns/docs/set-up-dns-records-domain-name>
   10. <https://github.com/googleapis/google-cloud-go/blob/main/domains/apiv1beta1/domains_client_example_test.go>
2. [SeeIP](https://seeip.org/)
3. Shelly
   1. <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt/#step-10-generate-periodic-updates-over-mqtt-using-shelly-script>
   2. <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt/#step-10-generate-periodic-updates-over-mqtt-using-shelly-script>
   3. <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/HTTP/>
4. Mochi-MQTT
   1. [github.com:mochi-mqtt/server](https://github.com/mochi-mqtt/server/tree/main)
   2. [Server with TLS](https://github.com/mochi-mqtt/server/blob/main/examples/tls/main.go)
5. GoLang
  1. <https://awesome-go.com/>
  1. <https://github.com/alexflint/go-arg>
  1. <https://github.com/spf13/cobra/blob/main/site/content/user_guide.md>
6. Internet Engineering Task Force (IETF)
  1. [RFC6762: Multicast DNS](https://datatracker.ietf.org/doc/html/rfc6762)
  2. [RFC6763: DNS-Based Service Discovery](https://datatracker.ietf.org/doc/html/rfc6763)
7. HiveMQ
   1. [MQTT Topics, Wildcards, & Best Practices – MQTT Essentials: Part 5](https://www.hivemq.com/blog/mqtt-essentials-part-5-mqtt-topics-best-practices/)
8. AWS
   1. [MQTT design best practices](https://docs.aws.amazon.com/whitepapers/latest/designing-mqtt-topics-aws-iot-core/mqtt-design-best-practices.html)
9. Cedalo
   1.  [The MQTT client and its role in the MQTT connection](https://cedalo.com/blog/mqtt-connection-beginners-guide)
       1.  [Persistent Sessions](https://cedalo.com/blog/mqtt-connection-beginners-guide/#mqtt-persistent-session)
   2.  [Essential Guide to MQTT Topics and Wildcards](https://cedalo.com/blog/mqtt-topics-and-mqtt-wildcards-explained/)
