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
  

event_2 = {
  "ble": {},
  "cloud": {
    "connected": true
  },
  "input:0": {
    "id": 0,
    "state": false
  },
  "mqtt": {
    "connected": true
  },
  "switch:0": {
    "id": 0,
    "output": true,
    "source": "SHC",
    "temperature": {
      "tC": 26.5,
      "tF": 79.6
    }
  },
  "sys": {
    "available_updates": {
      "stable": {
        "version": "1.3.3"
      }
    },
    "cfg_rev": 20,
    "fs_free": 151552,
    "fs_size": 458752,
    "kvs_rev": 0,
    "mac": "08B61FCFE6C0",
    "ram_free": 146432,
    "ram_size": 246452,
    "restart_required": false,
    "schedule_rev": 0,
    "time": "02:56",
    "unixtime": 1736819811,
    "uptime": 944772,
    "webhook_rev": 0
  },
  "ts": 1736819818.04,
  "wifi": {
    "rssi": -52,
    "ssid": "Linksys_7A50",
    "sta_ip": "192.168.1.66",
    "status": "got ip"
  },
  "ws": {
    "connected": false
  }
}

get_component_response = {
  "dst": "GIGARO_shellyplus1-b8d61a85a970",
  "error": null,
  "id": 0,
  "result": {
    "cfg_revision": 0,
    "components": [
      {
        "config": {
          "enable": true,
          "observer": {
            "enable": false
          },
          "rpc": {
            "enable": true
          }
        },
        "key": "ble",
        "status": {}
      },
      {
        "config": {
          "enable": true,
          "server": "shelly-78-eu.shelly.cloud:6022/jrpc"
        },
        "key": "cloud",
        "status": {
          "connected": true
        }
      },
      {
        "config": {
          "enable": true,
          "factory_reset": true,
          "id": 0,
          "invert": true,
          "name": null,
          "type": "switch"
        },
        "key": "input:0",
        "status": {
          "id": 0,
          "state": true
        }
      },
      {
        "config": {
          "client_id": "shellyplus1-b8d61a85a970",
          "enable": true,
          "enable_control": true,
          "enable_rpc": true,
          "rpc_ntf": true,
          "server": "192.168.1.2:1883",
          "status_ntf": true,
          "topic_prefix": "shellyplus1-b8d61a85a970",
          "use_client_cert": false,
          "user": null
        },
        "key": "mqtt",
        "status": {
          "connected": true
        }
      }
    ],
    "offset": 0,
    "total": 8
  },
  "src": "shellyplus1-b8d61a85a970"
}