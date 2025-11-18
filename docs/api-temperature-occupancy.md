# Temperature & Occupancy API Reference

## Overview

RPC API documentation for temperature and occupancy services. Both services integrate into the MyHome RPC system using MQTT transport.

- **Temperature Service**: SQLite-based temperature setpoint management with schedules
- **Occupancy Service**: Home occupancy detection based on input events and mobile device presence

## RPC Methods

### temperature.get

Get temperature configuration for a room.

**Request:**
```json
{
  "id": "cli-12345",
  "method": "temperature.get",
  "params": {
    "room_id": "living-room"
  }
}
```

**Response:**
```json
{
  "id": "cli-12345",
  "result": {
    "room_id": "living-room",
    "name": "Living Room",
    "comfort_temp": 21.0,
    "eco_temp": 17.0,
    "schedule": {
      "weekday": [
        {"start": "06:00", "end": "23:00"}
      ],
      "weekend": [
        {"start": "08:00", "end": "23:00"}
      ]
    }
  }
}
```

### temperature.set

Create or update temperature configuration for a room.

**Request:**
```json
{
  "id": "cli-12346",
  "method": "temperature.set",
  "params": {
    "room_id": "living-room",
    "name": "Living Room",
    "comfort_temp": 21.0,
    "eco_temp": 17.0,
    "weekday": ["06:00-23:00"],
    "weekend": ["08:00-23:00"]
  }
}
```

**Response:**
```json
{
  "id": "cli-12346",
  "result": {
    "status": "ok",
    "room_id": "living-room"
  }
}
```

### temperature.list

List all temperature configurations.

**Request:**
```json
{
  "id": "cli-12347",
  "method": "temperature.list",
  "params": null
}
```

**Response:**
```json
{
  "id": "cli-12347",
  "result": {
    "living-room": {
      "room_id": "living-room",
      "name": "Living Room",
      "comfort_temp": 21.0,
      "eco_temp": 17.0,
      "schedule": {...}
    },
    "bedroom": {
      "room_id": "bedroom",
      "name": "Bedroom",
      "comfort_temp": 19.0,
      "eco_temp": 16.0,
      "schedule": {...}
    }
  }
}
```

### temperature.delete

Delete temperature configuration for a room.

**Request:**
```json
{
  "id": "cli-12348",
  "method": "temperature.delete",
  "params": {
    "room_id": "living-room"
  }
}
```

**Response:**
```json
{
  "id": "cli-12348",
  "result": {
    "status": "ok",
    "room_id": "living-room"
  }
}
```

### temperature.getsetpoint

Get current temperature setpoint for a room based on schedule.

**Request:**
```json
{
  "id": "heater-1234567890",
  "method": "temperature.getsetpoint",
  "params": {
    "room_id": "living-room"
  }
}
```

**Response:**
```json
{
  "id": "heater-1234567890",
  "result": {
    "setpoint_comfort": 21.0,
    "setpoint_eco": 17.0,
    "active_setpoint": 21.0,
    "reason": "comfort_hours"
  }
}
```

**Reasons:**
- `comfort_hours` - Current time is within comfort schedule
- `eco_hours` - Current time is outside comfort schedule

### occupancy.getstatus

Get current home occupancy status.

**Request:**
```json
{
  "id": "heater-occ-1234567890",
  "method": "occupancy.getstatus",
  "params": null
}
```

**Response:**
```json
{
  "id": "heater-occ-1234567890",
  "result": {
    "occupied": true
  }
}
```

**Occupancy Detection:**
- Monitors Shelly Gen2 input events (buttons, motion sensors)
- Polls SFR Box for mobile device presence (configurable device names)
- Occupied if recent input activity OR mobile device seen online
- Configurable time window (default: 12 hours)

## Transport

All methods use MQTT RPC over the `myhome/rpc` topic with JSON-RPC 2.0 format:

**Request:**
```json
{
  "id": "unique-request-id",
  "method": "service.method",
  "params": { ... }
}
```

**Response:**
```json
{
  "id": "unique-request-id",
  "result": { ... }
}
```

**Error Response:**
```json
{
  "id": "unique-request-id",
  "error": {
    "code": -32000,
    "message": "Error description"
  }
}
```
