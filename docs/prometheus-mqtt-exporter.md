# Prometheus MQTT Exporter Configuration

This document describes how to configure the Prometheus MQTT exporter to consume metrics published by Shelly watchdog.js scripts.

## Overview

The Shelly `watchdog.js` script publishes device metrics in two formats:

1. **Prometheus text format** to `shelly/metrics/{device_id}` - for direct consumption by Prometheus or other tools
2. **Individual JSON metrics** to `shelly/{device_id}/{metric_name}` - for consumption by mqtt-prometheus-exporter

## Architecture

```
┌─────────────────┐
│ Shelly Device   │
│  (watchdog.js)  │
└────────┬────────┘
         │
         │ Publishes metrics every 30s
         ▼
┌─────────────────┐
│  MQTT Broker    │
└────────┬────────┘
         │
         │ Topics: shelly/{device_id}/{metric_name}
         │ Payload: {"value": <number>}
         ▼
┌─────────────────┐
│ mqtt-prometheus │
│    exporter     │
└────────┬────────┘
         │
         │ HTTP endpoint: http://localhost:8079/metrics
         ▼
┌─────────────────┐
│  Prometheus     │
└─────────────────┘
```

## Published Metrics

### System Metrics (from watchdog.js)

| Metric Name | Type | Description | MQTT Topic |
|-------------|------|-------------|------------|
| `shelly_uptime_seconds` | counter | System uptime in seconds | `shelly/{device_id}/uptime_seconds` |
| `shelly_ram_size_bytes` | gauge | Internal board RAM size in bytes | `shelly/{device_id}/ram_size_bytes` |
| `shelly_ram_free_bytes` | gauge | Internal board free RAM size in bytes | `shelly/{device_id}/ram_free_bytes` |

### Switch Metrics (from watchdog.js, per switch)

| Metric Name | Type | Description | MQTT Topic |
|-------------|------|-------------|------------|
| `shelly_switch_power_watts` | gauge | Instant power consumption in watts | `shelly/{device_id}/switch_{N}_power_watts` |
| `shelly_switch_voltage_volts` | gauge | Instant voltage in volts | `shelly/{device_id}/switch_{N}_voltage_volts` |
| `shelly_switch_current_amperes` | gauge | Instant current in amperes | `shelly/{device_id}/switch_{N}_current_amperes` |
| `shelly_switch_temperature_celsius` | gauge | Temperature of the device in celsius | `shelly/{device_id}/switch_{N}_temperature_celsius` |
| `shelly_switch_power_total` | counter | Accumulated energy consumed in watt-hours | `shelly/{device_id}/switch_{N}_power_total` |
| `shelly_switch_output` | gauge | Switch state (1=on, 0=off) | `shelly/{device_id}/switch_{N}_output` |
| `shelly_switch_activated_total` | counter | Total number of switch activation events (on transitions) | `shelly/{device_id}/switch_{N}_activated` |
| `shelly_switch_deactivated_total` | counter | Total number of switch deactivation events (off transitions) | `shelly/{device_id}/switch_{N}_deactivated` |

Where `{N}` is the switch number (e.g., `0`, `1`, etc.)

### Gen1 H&T Device Metrics

These metrics are published by Gen1 Shelly H&T devices via the Gen1 MQTT proxy.

| Metric Name | Type | Description | MQTT Topic |
|-------------|------|-------------|------------|
| `shelly_gen1_temperature_celsius` | gauge | Temperature from Gen1 H&T sensor in Celsius | `shellies/{device_id}/sensor/temperature` |
| `shelly_gen1_humidity_percent` | gauge | Humidity from Gen1 H&T sensor in percent | `shellies/{device_id}/sensor/humidity` |
| `shelly_gen1_battery_volts` | gauge | Battery voltage from Gen1 sensor in volts | `shellies/{device_id}/sensor/battery` |

**Note**: Gen1 topics use `shellies/` prefix (with 's'), not `shelly/`.

### BLU Device Metrics (BTHome Protocol)

These metrics are published by the blu-publisher.js script running on Shelly gateway devices.

| Metric Name | Type | Description | MQTT Topic |
|-------------|------|-------------|------------|
| `shelly_blu_temperature_celsius` | gauge | Temperature from Shelly BLU device in Celsius | `shelly-blu/events/{mac}` |
| `shelly_blu_humidity_percent` | gauge | Humidity from Shelly BLU device in percent | `shelly-blu/events/{mac}` |
| `shelly_blu_battery_percent` | gauge | Battery level from Shelly BLU device in percent | `shelly-blu/events/{mac}` |
| `shelly_blu_motion` | gauge | Motion detection (0=clear, 1=detected) | `shelly-blu/events/{mac}` |
| `shelly_blu_button_total` | counter | Button press count | `shelly-blu/events/{mac}` |
| `shelly_blu_window` | gauge | Window state (0=closed, 1=open) | `shelly-blu/events/{mac}` |
| `shelly_blu_illuminance_lux` | gauge | Illuminance in lux | `shelly-blu/events/{mac}` |
| `shelly_blu_rssi_dbm` | gauge | Signal strength in dBm | `shelly-blu/events/{mac}` |

Where `{mac}` is the BLU device MAC address (e.g., `e8:e0:7e:d0:f9:89`).

**BLU Device Types**:
- **SBHT** (Shelly BLU H&T): Temperature, Humidity, Battery
- **SBDW** (Shelly BLU Door/Window): Window, Battery
- **SBBT** (Shelly BLU Button): Button, Battery
- **SBMO** (Shelly BLU Motion): Motion, Illuminance, Battery

## Configuration File

The configuration file should be installed as `/etc/prometheus/mqtt-exporter.yaml` and will override the default configuration from the Debian `prometheus-mqtt-exporter` package.

### Location

```
/Users/fix/Desktop/GIT/home-automation/linux/prometheus/mqtt-exporter.yaml
```

### Installation

```bash
# Copy the configuration file to the system location
sudo cp linux/prometheus/mqtt-exporter.yaml /etc/prometheus/mqtt-exporter.yaml

# Reload systemd daemon and restart the service
sudo systemctl daemon-reload
sudo systemctl restart prometheus-mqtt-exporter

# Verify the service is running
sudo systemctl status prometheus-mqtt-exporter
```

**Note**: The service automatically reads `/etc/prometheus/mqtt-exporter.yaml` on startup. A simple restart is sufficient to apply configuration changes.

### Configuration Structure

The configuration file includes:

1. **Logging settings** - INFO level by default
2. **HTTP server** - Listens on port 8079 for Prometheus scraping
3. **MQTT client** - Connects to localhost:1883 by default
4. **Cache settings** - 60 second expiration (metrics published every 30s)
5. **Metrics definitions** - Maps MQTT topics to Prometheus metrics

### Key Configuration Options

#### MQTT Broker Connection

```yaml
mqtt:
  host: "tcp://localhost"  # Change to your MQTT broker address
  port: 1883               # Change to your MQTT broker port
  username: ""             # Optional authentication
  password: ""             # Optional authentication
  timeout: 3s
```

#### Metric Definition Example

```yaml
- mqtt_topic: "shelly/+/uptime_seconds"  # Topic pattern with wildcard
  prom_name: "shelly_uptime_seconds"     # Prometheus metric name
  type: "counter"                         # Metric type: gauge or counter
  help: "System uptime in seconds"        # Metric description
  json_field: "value"                     # Extract value from JSON payload
  topic_labels:                           # Extract labels from topic
    - device_id: 1                        # Position 1 in topic path
```

## Watchdog.js Configuration

The watchdog.js script must have `publishIndividualMetrics` enabled:

```javascript
prometheus: {
    enabled: true,
    publishIntervalSeconds: 30,
    mqttTopic: "shelly/metrics",
    monitoredSwitches: ["switch:0"],  // Add more switches as needed
    publishIndividualMetrics: true     // Required for mqtt-prometheus-exporter
}
```

**Features**:
- Publishes system metrics (uptime, RAM)
- Publishes switch metrics (power, voltage, current, temperature, energy)
- Tracks switch activation/deactivation events (state changes)
- Publishes both Prometheus text format and individual JSON metrics

## Prometheus Configuration

Add the mqtt-prometheus-exporter as a scrape target in your Prometheus configuration:

```yaml
scrape_configs:
  - job_name: 'shelly-devices'
    static_configs:
      - targets: ['localhost:8079']
```

## Verification

### Check MQTT Topics

Use the HiveMQ MQTT CLI to verify metrics are being published:

```bash
# Subscribe to all Shelly metrics from watchdog.js
mqtt sub -t 'shelly/#' -h localhost

# Subscribe to Gen1 H&T sensors
mqtt sub -t 'shellies/+/sensor/#' -h localhost

# Subscribe to BLU devices
mqtt sub -t 'shelly-blu/events/#' -h localhost

# Subscribe to a specific device
mqtt sub -t 'shelly/shellyplus1pm-a8032ab12345/#' -h localhost
```

### Check Exporter Metrics

```bash
# View all metrics exposed by the exporter
curl http://localhost:8079/metrics

# Filter for Shelly metrics
curl http://localhost:8079/metrics | grep shelly_
```

### Check Exporter Logs

```bash
# View exporter service logs
sudo journalctl -u prometheus-mqtt-exporter -f
```

## Troubleshooting

### No metrics appearing

1. Check that watchdog.js is running on the Shelly device
2. Verify MQTT broker is running and accessible
3. Check that `publishIndividualMetrics` is enabled in watchdog.js config
4. Verify MQTT topics are being published: `mqtt sub -t 'shelly/#' -h localhost`

### Wrong metric values

1. Check the JSON payload format: should be `{"value": <number>}`
2. Verify the `json_field` in the exporter config is set to `"value"`
3. Check exporter logs for parsing errors

### Metrics not updating

1. Verify the cache expiration is set correctly (60s for 30s publish interval)
2. Check that the MQTT broker is delivering messages
3. Restart the mqtt-prometheus-exporter service

## Adding More Switches

To monitor additional switches, add corresponding metric definitions to the configuration file for each metric type:

```yaml
# Switch 1 metrics (repeat for each metric: power, voltage, current, temperature, total, output, activated, deactivated)
- mqtt_topic: "shelly/+/switch_1_power_watts"
  prom_name: "shelly_switch_power_watts"
  type: "gauge"
  help: "Instant power consumption in watts"
  json_field: "value"
  const_labels:
    - switch: "switch:1"
  topic_labels:
    - device_id: 1
```

And update the watchdog.js configuration:

```javascript
monitoredSwitches: ["switch:0", "switch:1"]
```

## Data Flow Summary

### Watchdog.js (on Shelly devices)
1. Collects system and switch metrics every 30 seconds
2. Tracks switch state changes for activation/deactivation events
3. Publishes individual metrics as JSON to `shelly/{device_id}/{metric_name}`
4. Also publishes Prometheus text format to `shelly/metrics/{device_id}`

### Gen1 Proxy (in myhome daemon)
1. Receives Gen1 device data via HTTP
2. Publishes to `shellies/{device_id}/sensor/{metric}` as plain values

### BLU Publisher (on Shelly gateway devices)
1. Scans for BLU devices via Bluetooth
2. Decodes BTHome protocol data
3. Publishes to `shelly-blu/events/{mac}` as JSON

### MQTT Prometheus Exporter
1. Subscribes to all metric topics
2. Extracts values from JSON payloads or plain values
3. Exposes metrics at `http://localhost:8079/metrics`
4. Prometheus scrapes this endpoint

## References

- [mqtt-prometheus-exporter GitHub](https://github.com/torilabs/mqtt-prometheus-exporter)
- [Prometheus Documentation](https://prometheus.io/docs/)
- [HiveMQ MQTT CLI](https://hivemq.github.io/mqtt-cli/)
