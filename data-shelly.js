

// curl -X POST -d '{"id":1,"method":"HTTP.GET","params":{"url":"http://192.168.33.18/rpc/Shelly.GetDeviceInfo"}}' http://Shelly1MiniG3-54320464A1D0.local/rpc

var shelly_http_proxied_getdeviceinfo_response = {
  "id": 1,
  "src": "shelly1minig3-54320464a1d0",
  "result": {
    "code": 200,
    "message": "OK",
    "headers": {
      "Connection": "close",
      "Content-Length": "254",
      "Content-Type": "application/json",
      "Server": "ShellyHTTP/1.0.0"
    },
    "body": "{\"name\":\"lumiere-escalier\",\"id\":\"shelly1minig3-54320464f17c\",\"mac\":\"54320464F17C\",\"slot\":1,\"model\":\"S3SW-001X8EU\",\"gen\":3,\"fw_id\":\"20231121-110944/1.1.99-minig3prod1-ga898543\",\"ver\":\"1.1.99-minig3prod1\",\"app\":\"Mini1G3\",\"auth_en\":false,\"auth_domain\":null}"
  }
}

var shelly_http_proxied_response_body = {
  "name": "lumiere-escalier",
  "id": "shelly1minig3-54320464f17c",
  "mac": "54320464F17C",
  "slot": 1,
  "model": "S3SW-001X8EU",
  "gen": 3,
  "fw_id": "20231121-110944/1.1.99-minig3prod1-ga898543",
  "ver": "1.1.99-minig3prod1",
  "app": "Mini1G3",
  "auth_en": false,
  "auth_domain": null
}

// curl -X POST -d '{"id":1,"method":"HTTP.GET","params":{"url":"http://192.168.33.18/rpc/Shelly.Update"}}' http://Shelly1MiniG3-54320464A1D0.local/rpc

var shelly_http_proxied_update_response = {
  "id": 1,
  "src": "shelly1minig3-54320464a1d0",
  "result": {
    "code": 200,
    "message": "OK",
    "headers": {
      "Connection": "close",
      "Content-Length": "4",
      "Content-Type": "application/json",
      "Server": "ShellyHTTP/1.0.0"
    },
    "body": "null"
  }
}

// curl -X POST -d '{"id":1,"method":"HTTP.GET","params":{"url":"http://192.168.33.18/rpc/WiFi.GetConfig"}}' http://Shelly1MiniG3-54320464A1D0.local/rpc

var shelly_http_proxied_wifi_getconfig_response = {
  "id": 1,
  "src": "shelly1minig3-54320464a1d0",
  "result": {
    "code": 200,
    "message": "OK",
    "headers": {
      "Connection": "close",
      "Content-Length": "433",
      "Content-Type": "application/json",
      "Server": "ShellyHTTP/1.0.0"
    },
    "body": "{\"ap\":{\"ssid\":\"Shelly1MiniG3-54320464F17C\",\"is_open\":false, \"enable\":true, \"range_extender\": {\"enable\":false}},\"sta\":{\"ssid\":\"Shelly1MiniG3-54320464A1D0\",\"is_open\":false, \"enable\":true, \"ipv4mode\":\"dhcp\",\"ip\":null,\"netmask\":null,\"gw\":null,\"nameserver\":null},\"sta1\":{\"ssid\":\"FiX Work iPhone\",\"is_open\":false, \"enable\":true, \"ipv4mode\":\"dhcp\",\"ip\":null,\"netmask\":null,\"gw\":null,\"nameserver\":null},\"roam\":{\"rssi_thr\":-80,\"interval\":60}}"
  }
}

var shelly_http_proxied_wifi_getconfig_response_body = {
  "ap": {
    "ssid": "Shelly1MiniG3-54320464F17C",
    "is_open": false,
    "enable": true,
    "range_extender": {
      "enable": false
    }
  },
  "sta": {
    "ssid": "Shelly1MiniG3-54320464A1D0",
    "is_open": false,
    "enable": true,
    "ipv4mode": "dhcp",
    "ip": null,
    "netmask": null,
    "gw": null,
    "nameserver": null
  },
  "sta1": {
    "ssid": "FiX Work iPhone",
    "is_open": false,
    "enable": true,
    "ipv4mode": "dhcp",
    "ip": null,
    "netmask": null,
    "gw": null,
    "nameserver": null
  },
  "roam": {
    "rssi_thr": -80,
    "interval": 60
  }
}

var shelly_pro3_evt_wifi_connecting = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1745667977.96,
    "wifi": {
      "rssi": 0,
      "ssid": null,
      "sta_ip": "0.0.0.0",
      "status": "connecting"
    }
  }
}

var shelly_pro3_evt_wifi_disconnected = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyEvent",
  "params": {
    "ts": 1745666408.35,
    "events": [
      {
        "component": "wifi",
        "event": "sta_disconnected",
        "sta_ip": null,
        "ssid": null,
        "reason": 8,
        "ts": 1745666408.35
      }
    ]
  }
}

var shelly_pro3_evt_mqtt_connected = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116317.37,
    "mqtt": {
      "connected": true
    }
  }
}

var shelly_pro3_evt_wifi_connected = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116468.60,
    "wifi": {
      "rssi": -95,
      "ssid": "Linksys_7A50",
      "sta_ip": "0.0.0.0",
      "status": "connected"
    }
  }
}

var shelly_pro3_input0_true = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116501.28,
    "input:0": {
      "id": 0,
      "state": true
    }
  }
}

var shelly_pro3_input0_false = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116508.89,
    "input:0": {
      "id": 0,
      "state": false
    }
  }
}

var shelly_pro3_switch0_true = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116501.28,
    "switch:0": {
      "id": 0,
      "output": true,
      "source": "switch"
    }
  }
}

var shelly_pro3_switch0_false = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116508.89,
    "switch:0": {
      "id": 0,
      "output": false,
      "source": "switch"
    }
  }
}

var shelly_pro3_switch0_source_http = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116508.89,
    "switch:0": {
      "id": 0,
      "output": false,
      "source": "HTTP"
    }
  }
}

var shelly_pro3_input0_source_switch = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116508.89,
    "input:0": {
      "id": 0,
      "state": false,
      "source": "switch"
    }
  }
}

var shelly_pro3_input0_source_mqtt = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116508.89,
    "input:0": {
      "id": 0,
      "state": false,
      "source": "MQTT"
    }
  }
}

  var shelly_pro3_switch0_source_switch = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116508.89,
    "switch:0": {
      "id": 0,
      "output": false,
      "source": "switch"
    }
  }
}

var shelly_pro3_switch0_source_mqtt = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746116508.89,
    "switch:0": {
      "id": 0,
      "output": false,
      "source": "MQTT"
    }
  }
}

var shelly_pro3_switch0_config_changed = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyEvent",
  "params": {
    "ts": 1746117052.62,
    "events": [
      {
        "component": "switch:0",
        "id": 0,
        "event": "config_changed",
        "restart_required": false,
        "ts": 1746117052.62,
        "cfg_rev": 25
      }
    ]
  }
}

var shelly_pro3_sys_cfg_rev_25 = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746117052.62,
    "sys": {
      "cfg_rev": 25
    }
  }
}

var shelly_pro3_switch2_config_changed = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyEvent",
  "params": {
    "ts": 1746117176.14,
    "events": [
      {
        "component": "switch:2",
        "id": 2,
        "event": "config_changed",
        "restart_required": false,
        "ts": 1746117176.14,
        "cfg_rev": 27
      }
    ]
  }
}

var shelly_pro3_sys_cfg_rev_27 = {
  "src": "shellypro3-a0dd6ca1c588",
  "dst": "shellypro3-a0dd6ca1c588/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1746117176.14,
    "sys": {
      "cfg_rev": 27
    }
  }
}

var shelly_pro2 = {
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
    "cloud": {
      "enable": true,
      "server": "shelly-78-eu.shelly.cloud:6022/jrpc"
    },
    "input:0": {
      "auto_off": false,
      "auto_off_delay": 0,
      "auto_on": false,
      "auto_on_delay": 0,
      "id": 0,
      "in_mode": "",
      "initial_state": "",
      "name": null
    },
    "input:1": {
      "auto_off": false,
      "auto_off_delay": 0,
      "auto_on": false,
      "auto_on_delay": 0,
      "id": 1,
      "in_mode": "",
      "initial_state": "",
      "name": null
    },
    "mqtt": {
      "client_id": "shellypro2-2cbcbb9fb834",
      "enable": false,
      "enable_control": true,
      "enable_rpc": true,
      "rpc_ntf": true,
      "status_ntf": false,
      "topic_prefix": "shellypro2-2cbcbb9fb834",
      "use_client_cert": false
    },
    "switch:0": {
      "auto_off": false,
      "auto_off_delay": 60,
      "auto_on": false,
      "auto_on_delay": 60,
      "id": 0,
      "in_mode": "momentary",
      "initial_state": "off"
    },
    "switch:1": {
      "auto_off": false,
      "auto_off_delay": 60,
      "auto_on": false,
      "auto_on_delay": 60,
      "id": 1,
      "in_mode": "momentary",
      "initial_state": "off"
    },
    "wifi": {
      "ap": {
        "password": "",
        "ssid": "ShellyPro2-2CBCBB9FB834"
      },
      "mode": "",
      "password": "",
      "ssid": "",
      "sta": {
        "password": "",
        "ssid": "Linksys_7A50"
      },
      "sta1": {
        "password": "",
        "ssid": ""
      }
    },
    "ws": {
      "enable": false,
      "server": null,
      "ssl_ca": "ca.pem"
    }
  },
  "config_revision": 18,
  "host": "192.168.1.42",
  "id": "shellypro2-2cbcbb9fb834",
  "info": {
    "app": "Pro2",
    "auth_en": false,
    "discoverable": false,
    "fw_id": "20240625-122917/1.3.3-gbdfd9b3",
    "gen": 2,
    "id": "shellypro2-2cbcbb9fb834",
    "mac": "2CBCBB9FB834",
    "model": "SPSW-202XE12UL",
    "ver": "1.3.3"
  },
  "mac": "d8:20:42:04:1f:45:07:cd:f8",
  "manufacturer": "Shelly",
  "name": ""
}

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

var shelly_minig3_evt_reboot = {
  "src": "shelly1minig3-54320464a1d0",
  "dst": "shelly1minig3-54320464a1d0/events",
  "method": "NotifyEvent",
  "params": {
    "ts": 1745409198.28,
    "events": [
      {
        "component": "sys",
        "event": "scheduled_restart",
        "time_ms": 996,
        "ts": 1745409198.28
      }
    ]
  }
}

var shelly_minig3_get_wifi_list_ap_clients = {
  "id": 0,
  "src": "shelly1minig3-54320464a1d0",
  "dst": "homectl-viganj.local-24434_shelly1minig3-54320464a1d0",
  "result": {
    "ts": 1745693071,
    "ap_clients": [
      {
        "mac": "54:32:04:64:19:f8",
        "ip": "192.168.33.29",
        "ip_static": false,
        "mport": 12552,
        "since": 1745650958
      },
      {
        "mac": "54:32:04:64:f1:7c",
        "ip": "192.168.33.18",
        "ip_static": false,
        "mport": 10380,
        "since": 1745509881
      }
    ]
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

var shelly_plus1_switch_toggle_notify_status = {
  "src": "shellyplus1-b8d61a85a8e0",
  "dst": "shellyplus1-b8d61a85a8e0/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1728655743.99,
    "switch:0": {
      "id": 0,
      "output": true,
      "source": "button"
    }
  }
}

var shelly_plus1_switch_toggle_notify_event_btn_down = {
  "src": "shellyplus1-b8d61a85a8e0",
  "dst": "shellyplus1-b8d61a85a8e0/events",
  "method": "NotifyEvent",
  "params": {
    "ts": 1728655743.99,
    "events": [
      {
        "component": "input:0",
        "id": 0,
        "event": "btn_down",
        "ts": 1728655743.99
      }
    ]
  }
}

var shelly_plus1_switch_toggle_notify_event_btn_up = {
  "src": "shellyplus1-b8d61a85a8e0",
  "dst": "shellyplus1-b8d61a85a8e0/events",
  "method": "NotifyEvent",
  "params": {
    "ts": 1728655744.17,
    "events": [
      {
        "component": "input:0",
        "id": 0,
        "event": "btn_up",
        "ts": 1728655744.17
      }
    ]
  }
}

var shelly_plus1_switch_toggle_notify_event_single_push = {
  "src": "shellyplus1-b8d61a85a8e0",
  "dst": "shellyplus1-b8d61a85a8e0/events",
  "method": "NotifyEvent",
  "params": {
    "ts": 1728655744.49,
    "events": [
      {
        "component": "input:0",
        "id": 0,
        "event": "single_push",
        "ts": 1728655744.49
      }
    ]
  }
}

var shelly_plus1_switch_toggle_notify_status = {
  "src": "shellyplus1-b8d61a85a8e0",
  "dst": "shellyplus1-b8d61a85a8e0/events",
  "method": "NotifyStatus",
  "params": {
    "ts": 1728655745.60,
    "switch:0": {
      "id": 0,
      "output": false,
      "source": "button"
    }
  }
}
