bluMotionSensorEventData = {
    "encryption": false,
    "BTHome_version": 2,
    "pid": 105,
    "battery": 98,
    "illuminance": 174,
    "motion": 1,
    "rssi": -70,
    "address": "e8:e0:7e:d0:f9:89",
    "model": undefined
}

switch4IEventData = [{
    "info": {
        "component": "input:1",
        "id": 1,
        "event": "toggle",
        "state": true,
        "ts": 1732463413.75999999046
    }
},
{
    "id": 1, "now": 1732463414.02464318275,
    "info": {
        "component": "input:1",
        "id": 1,
        "event": "toggle",
        "state": false,
        "ts": 1732463414.01999998092
    }
},

{
    "id": 0, "now": 1732463414.73114681243,
    "info": {
        "component": "input:0",
        "id": 0,
        "event": "toggle",
        "state": true,
        "ts": 1732463414.73000001907
    }
},
{
    "id": 0, "now": 1732463414.95820188522,
    "info": {
        "component": "input:0",
        "id": 0,
        "event": "toggle",
        "state": false,
        "ts": 1732463414.96000003814
    }
},
{
    "id": 2, "now": 1732463415.58661103248,
    "info": {
        "component": "input:2",
        "id": 2,
        "event": "toggle",
        "state": true,
        "ts": 1732463415.58999991416
    }
},
{
    "id": 2, "now": 1732463415.85527706146,
    "info": {
        "component": "input:2",
        "id": 2,
        "event": "toggle",
        "state": false,
        "ts": 1732463415.85999989509
    }
},
{
    "id": 3, "now": 1732463416.45232319831,
    "info": {
        "component": "input:3",
        "id": 3,
        "event": "toggle",
        "state": true,
        "ts": 1732463416.45000004768
    }
},
{
    "id": 3, "now": 1732463416.65927505493,
    "info": {
        "component": "input:3",
        "id": 3,
        "event": "toggle",
        "state": false,
        "ts": 1732463416.65999984741
    }
}]

scriptReloadEventData = [{
    "id": -1, "now": 1732473181.38573503494,
    "info": {
        "component": "sys",
        "event": "component_removed",
        "target": "script:3",
        "restart_required": false,
        "ts": 1732473181.38999986648,
        "cfg_rev": 58
    }
}, {
    "id": -1,
    "now": 1732473209.48322010040,
    "info": {
        "component": "sys",
        "event": "component_added",
        "target": "script:3",
        "restart_required": false,
        "ts": 1732473209.48000001907,
        "cfg_rev": 59
    }
}]

mqtt_event_bluetooth = {
    "src": "shelly1minig3-5432046419f8",
    "dst": "shelly1minig3-5432046419f8/events",
    "method": "NotifyFullStatus",
    "params": {
      "ts": 1736633812.50,
      "ble": {},
      "bthome": {
        "errors": [
          "observer_disabled"
        ]
      },
      "cloud": {
        "connected": false
      },
      "input:0": {
        "id": 0,
        "state": false
      },
      "knx": {},
      "mqtt": {
        "connected": true
      },
      "switch:0": {
        "id": 0,
        "source": "SHC",
        "output": false,
        "temperature": {
          "tC": 51.2,
          "tF": 124.2
        }
      },
      "sys": {
        "mac": "5432046419F8",
        "restart_required": false,
        "time": "23:16",
        "unixtime": 1736633812,
        "uptime": 757645,
        "ram_size": 259400,
        "ram_free": 82468,
        "fs_size": 1048576,
        "fs_free": 589824,
        "cfg_rev": 22,
        "kvs_rev": 0,
        "schedule_rev": 1,
        "webhook_rev": 0,
        "available_updates": {
          "beta": {
            "version": "1.5.0-beta1"
          }
        },
        "reset_reason": 1
      },
      "wifi": {
        "sta_ip": "192.168.33.29",
        "status": "got ip",
        "ssid": "Shelly1MiniG3-54320464A1D0",
        "rssi": -92
      },
      "ws": {
        "connected": false
      }
    }
  }
  
  event_1 = {
    "src": "shelly1minig3-54320464a1d0",
    "dst": "shelly1minig3-54320464a1d0/events",
    "method": "NotifyEvent",
    "params": {
      "ts": 1736605194.11,
      "events": [
        {
          "component": "input:0",
          "id": 0,
          "event": "config_changed",
          "restart_required": false,
          "ts": 1736605194.11,
          "cfg_rev": 35
        }
      ]
    }
  }
  