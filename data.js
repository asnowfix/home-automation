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

device_1 =     {
  "manufacturer": "Shelly",
  "id": "shellyplus1-08b61fd9d708",
  "mac": "08B61FD9D708",
  "host": "<nil>",
  "name": "",
  "config_revision": 0,
  "info": {
    "model": "SNSW-001X16EU",
    "mac": "08B61FD9D708",
    "app": "Plus1",
    "ver": "1.0.8",
    "gen": 2,
    "id": "shellyplus1-08b61fd9d708",
    "fw_id": "20231106-160328/1.0.8-gdba0ee3",
    "auth_en": false,
    "discoverable": false,
    "key": "eyJhbGciOiJFUzM4NCIsInR5cCI6IkpXVCJ9.eyJpYXQiOjE2NzExNjE1NjAsIm1hYyI6IjA4QjYxRkQ5RDcwOCIsIm0iOiJTTlNXLTAwMVgxNkVVIiwiYiI6IjIyMzktQnJvYWR3ZWxsIiwiZnAiOiIwNzYzZGRhMiJ9.zWOaAb9aD8_1A4nY6vwiyvNz0NlJzVlURsQxJB2FMLY1aCVFSboqhdqgls_M-PxBfYGY20mRpz2JOP65M7Mo02IHxTbB5Fhk3wSJn0Y3fWTYz_h5GEZabziWcjZUazso",
    "batch": "2239-Broadwell",
    "fw_sbits": "04"
  },
  "config": {
    "ble": {
      "enable": true,
      "observer": {
        "enable": false
      },
      "rpc": {
        "enable": true
      }
    },
    "bthome": null,
    "cloud": {
      "enable": true,
      "server": "shelly-78-eu.shelly.cloud:6022/jrpc"
    },
    "knx": null,
    "mqtt": {
      "enable": true,
      "server": "192.168.1.2:1883",
      "client_id": "shellyplus1-08b61fd9d708",
      "topic_prefix": "shellyplus1-08b61fd9d708",
      "rpc_ntf": true,
      "status_ntf": true,
      "use_client_cert": false,
      "enable_control": true
    },
    "switch:0": {
      "id": 0,
      "name": "Radiateur Chambre Aline",
      "in_mode": "follow",
      "initial_state": "on",
      "auto_on": false,
      "auto_on_delay": 60,
      "auto_off": false,
      "auto_off_delay": 60
    },
    "switch:1": null,
    "switch:2": null,
    "switch:3": null,
    "system": null,
    "wifi": {
      "mode": "",
      "ssid": "",
      "password": "",
      "ap": {
        "ssid": "ShellyPlus1-08B61FD9D708",
        "password": ""
      },
      "sta": {
        "ssid": "Linksys_7A50",
        "password": ""
      },
      "sta1": {
        "ssid": "",
        "password": ""
      }
    },
    "ws": {
      "enable": false,
      "server": null,
      "ssl_ca": "ca.pem"
    }
  },
  "status": {
    "ble": null,
    "bthome": null,
    "cloud": null,
    "input:0": null,
    "input:1": null,
    "input:2": null,
    "input:3": null,
    "knx": null,
    "mqtt": null,
    "switch:0": {
      "input": {
        "id": 0,
        "state": false
      },
      "id": 0,
      "source": "",
      "output": false,
      "pf": 0,
      "freq": 0,
      "aenergy": {
        "total": 0,
        "by_minute": null,
        "minute_ts": 0
      },
      "temperature": {
        "tC": 41.67,
        "tF": 107
      },
      "errors": null
    },
    "switch:1": null,
    "switch:2": null,
    "switch:3": null,
    "system": null,
    "wifi": null,
    "ws": null
  }
}

get_config_response = {
  "dst": "myhome_shelly1minig3-543204522cb4",
  "error": null,
  "id": 0,
  "result": {
    "ble": {
      "enable": true,
      "observer": {
        "enable": true
      },
      "rpc": {
        "enable": true
      }
    },
    "bthome": {},
    "cloud": {
      "enable": true,
      "server": "shelly-78-eu.shelly.cloud:6022/jrpc"
    },
    "input:0": {
      "enable": true,
      "factory_reset": true,
      "id": 0,
      "invert": false,
      "name": null,
      "type": "button"
    },
    "knx": {
      "enable": false,
      "ia": "15.15.255",
      "routing": {
        "addr": "224.0.23.12:3671"
      }
    },
    "mqtt": {
      "client_id": "shelly1minig3-543204522cb4",
      "enable": true,
      "enable_control": true,
      "enable_rpc": true,
      "rpc_ntf": true,
      "server": "192.168.1.2:1883",
      "ssl_ca": null,
      "status_ntf": true,
      "topic_prefix": "shelly1minig3-543204522cb4",
      "use_client_cert": false,
      "user": null
    },
    "script:1": {
      "enable": true,
      "id": 1,
      "name": "ble-shelly-motion.js"
    },
    "switch:0": {
      "auto_off": false,
      "auto_off_delay": 60,
      "auto_on": false,
      "auto_on_delay": 60,
      "id": 0,
      "in_mode": "detached",
      "initial_state": "off",
      "name": "Lumiere Porte Entree"
    },
    "sys": {
      "cfg_rev": 52,
      "debug": {
        "file_level": null,
        "level": 2,
        "mqtt": {
          "enable": false
        },
        "udp": {
          "addr": null
        },
        "websocket": {
          "enable": true
        }
      },
      "device": {
        "discoverable": true,
        "eco_mode": false,
        "fw_id": "20241011-114456/1.4.4-g6d2a586",
        "mac": "543204522CB4",
        "name": "front-door-light"
      },
      "location": {
        "lat": 43.6611,
        "lon": 6.9808,
        "tz": "Europe/Paris"
      },
      "rpc_udp": {
        "dst_addr": null,
        "listen_port": null
      },
      "sntp": {
        "server": "time.google.com"
      },
      "ui_data": {}
    },
    "wifi": {
      "ap": {
        "enable": false,
        "is_open": true,
        "range_extender": {
          "enable": false
        },
        "ssid": "Shelly1MiniG3-543204522CB4"
      },
      "roam": {
        "interval": 60,
        "rssi_thr": -80
      },
      "sta": {
        "enable": true,
        "gw": null,
        "ip": null,
        "ipv4mode": "dhcp",
        "is_open": false,
        "nameserver": null,
        "netmask": null,
        "ssid": "Linksys_7A50"
      },
      "sta1": {
        "enable": false,
        "gw": null,
        "ip": null,
        "ipv4mode": "dhcp",
        "is_open": true,
        "nameserver": null,
        "netmask": null,
        "ssid": null
      }
    },
    "ws": {
      "enable": false,
      "server": null,
      "ssl_ca": "ca.pem"
    }
  },
  "src": "shelly1minig3-543204522cb4"
}