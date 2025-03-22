var shellyplugsg3_get_components = {
  "id": 0,
  "src": "shellyplugsg3-b08184a53f24",
  "dst": "__debug_bin1660191309-palmbeach-405485_shellyplugsg3-b08184a53f24",
  "result": {
    "components": [
      {
        "key": "ble",
        "config": {
          "enable": true,
          "rpc": {
            "enable": true
          },
          "observer": {
            "enable": false
          }
        }
      },
      {
        "key": "cloud",
        "config": {
          "enable": true,
          "server": "shelly-78-eu.shelly.cloud:6022/jrpc"
        }
      },
      {
        "key": "mqtt",
        "config": {
          "enable": true,
          "server": "192.168.1.2",
          "client_id": "shellyplugsg3-b08184a53f24",
          "user": null,
          "ssl_ca": null,
          "topic_prefix": "shellyplugsg3-b08184a53f24",
          "rpc_ntf": true,
          "status_ntf": true,
          "use_client_cert": false,
          "enable_rpc": true,
          "enable_control": true
        }
      },
      {
        "key": "plugs_ui",
        "config": {
          "leds": {
            "mode": "power",
            "colors": {
              "switch:0": {
                "on": {
                  "rgb": [
                    0,
                    100,
                    0
                  ],
                  "brightness": 100
                },
                "off": {
                  "rgb": [
                    100,
                    0,
                    0
                  ],
                  "brightness": 100
                }
              },
              "power": {
                "brightness": 100
              }
            },
            "night_mode": {
              "enable": false,
              "brightness": 100,
              "active_between": []
            }
          },
          "controls": {
            "switch:0": {
              "in_mode": "momentary"
            }
          }
        }
      }
    ],
    "cfg_rev": 13,
    "offset": 0,
    "total": 8
  }
}

result = {
  "ble": {
    "enable": true,
    "observer": {
      "enable": false
    },
    "rpc": {
      "enable": true
    }
  },
  "cloud": {
    "enable": true,
    "server": "shelly-78-eu.shelly.cloud:6022/jrpc"
  },
  "input:0": {
    "enable": true,
    "factory_reset": true,
    "id": 0,
    "invert": true,
    "name": null,
    "type": "switch"
  },
  "mqtt": {
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
  "switch:0": {
    "auto_off": false,
    "auto_off_delay": 60,
    "auto_on": false,
    "auto_on_delay": 60,
    "id": 0,
    "in_mode": "follow",
    "initial_state": "on",
    "name": "Radiateur Bureau"
  },
  "sys": {
    "cfg_rev": 30,
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
        "enable": false
      }
    },
    "device": {
      "addon_type": null,
      "discoverable": true,
      "eco_mode": true,
      "fw_id": "20231031-152227/1.0.7-g5db02bd",
      "mac": "B8D61A85A970",
      "name": "radiateur-bureau"
    },
    "location": {
      "lat": 43.699,
      "lon": 6.9866,
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
      "enable": true,
      "is_open": false,
      "range_extender": {
        "enable": true
      },
      "ssid": "ShellyPlus1-B8D61A85A970"
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
}


results_sys_get_config_1 = {
  "id": 0,
  "src": "shelly1minig3-543204522cb4",
  "dst": "__debug_bin1505241838-palmbeach-2543369_shelly1minig3-543204522cb4",
  "result": {
    "device": {
      "name": "front-door-light",
      "mac": "543204522CB4",
      "fw_id": "20241011-114456/1.4.4-g6d2a586",
      "discoverable": true,
      "eco_mode": false
    },
    "location": {
      "tz": "Europe/Paris",
      "lat": 43.6611,
      "lon": 6.9808
    },
    "debug": {
      "level": 2,
      "file_level": null,
      "mqtt": {
        "enable": false
      },
      "websocket": {
        "enable": true
      },
      "udp": {
        "addr": null
      }
    },
    "ui_data": {},
    "rpc_udp": {
      "dst_addr": null,
      "listen_port": null
    },
    "sntp": {
      "server": "time.google.com"
    },
    "cfg_rev": 52
  }
}

results_sys_get_config_2 = {
  "id": 0,
  "src": "shelly1minig3-543204522cb4",
  "dst": "__debug_bin331115640-palmbeach-2545289_shelly1minig3-543204522cb4",
  "result": {
    "device": {
      "name": "front-door-light",
      "mac": "543204522CB4",
      "fw_id": "20241011-114456/1.4.4-g6d2a586",
      "discoverable": true,
      "eco_mode": false
    },
    "location": {
      "tz": "Europe/Paris",
      "lat": 43.6611,
      "lon": 6.9808
    },
    "debug": {
      "level": 2,
      "file_level": null,
      "mqtt": {
        "enable": false
      },
      "websocket": {
        "enable": true
      },
      "udp": {
        "addr": null
      }
    },
    "ui_data": {},
    "rpc_udp": {
      "dst_addr": null,
      "listen_port": null
    },
    "sntp": {
      "server": "time.google.com"
    },
    "cfg_rev": 52
  }
}