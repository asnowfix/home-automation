
var shelly_blu_event = {
  "encryption":false,
  "BTHome_version":2,
  "pid":56,
  "battery":89,
  "illuminance":709,
  "motion":1,
  "rssi":-71,
  "address":"e8:e0:7e:a6:0c:6f"
}

var shelly_plugsg3_get_status_response_with_exception = {
  "id": 1,
  "running": false,
  "mem_free": 25200,
  "cpu": 0,
  "errors": [
    "syntax_error"
  ],
  "error_msg": "Uncaught SyntaxError: Got TEMPLATE LITERAL expected EOF\n at ...,this.metricPrefix,e,\" \",s,`\n                              ^\nin function \"printPrometheusMetric\" called from ...uptime in seconds\",e.uptime),this.printPrometheusMetric(\"ram...\n                              ^\nin function \"generateMetricsForSystem\" called from ...s.generateMetricsForSystem(),this.generateMetricsForSwitches...\n                              ^\nin function \"httpServerHandler\" called from PrometheusMe..."
}

var shelly_plugsg3_get_deviceinfo_response = {
  "name": null,
  "id": "shellyplugsg3-28372f2dc824",
  "mac": "28372F2DC824",
  "slot": 1,
  "model": "S3PL-00112EU",
  "gen": 3,
  "fw_id": "20240820-134301/1.2.3-plugsg3prod0-gec79607",
  "ver": "1.2.3-matter22",
  "app": "PlugSG3",
  "auth_en": false,
  "auth_domain": null,
  "matter": true
}

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

// topic = "shelly1minig3-54320464074c/debug/log"
var debug_log = {}

// MqttPublish{topic=shelly1minig3-54320464074c/debug/log, payload=146byte, qos=AT_MOST_ONCE, retain=false, messageExpiryInterval=86400}
// shelly1minig3-54320464074c 31 1746800591.367 1|shos_dns_sd_respond:236 wa(0x3fcbf358): Announced Shelly1MiniG3-54320464074C any@any (192.168.33.1)
// Client 'd0f0uv2k604ou5ij8ec0@192.168.1.2' received PUBLISH ('shelly1minig3-54320464074c 32 1746800591.386 1|shos_dns_sd_respond:236 ws(0x3fcca9d0): Announced Shelly1MiniG3-54320464074C any@any (192.168.34.3)')

// MqttPublish{topic=shelly1minig3-54320464074c/debug/log, payload=139byte, qos=AT_MOST_ONCE, retain=false, messageExpiryInterval=86400}
// shelly1minig3-54320464074c 565 1746827545.392 1|shelly_user_script.:250 UserScript.HandleError (script:2) [2] syntax_error Error in EjsCall
// Client 'd0f7gt2k604ou5ij8ee0@192.168.1.2' received PUBLISH ('shelly1minig3-54320464074c 566 1746827545.424 1|shelly_notification:165 Status change of script:2: {"id":2,"error_msg":"Uncaught SyntaxError: Expecting a valid value, got ID\n at line 1 col 1\n[object Object]\n^\nin function called from system\n\n","errors":["syntax_error"],"running":false}')

// MqttPublish{topic=shelly1minig3-54320464074c/debug/log, payload=132byte, qos=AT_MOST_ONCE, retain=false, messageExpiryInterval=86400}
// shelly1minig3-54320464074c 723 1746829345.669 1|shelly_user_script.:250 UserScript.HandleError (script:2) [9] error Error in EjsCall
// Client 'd0f7gt2k604ou5ij8ee0@192.168.1.2' received PUBLISH ('shelly1minig3-54320464074c 724 1746829345.716 1|shelly_notification:165 Status change of script:2: {"id":2,"error_msg":"Uncaught Error: Function \"error\" not found!\n at                 console.error(\"Error: Can not parse allowed ...\n                        ^\nin function called from system\n\n","errors":["error"],"running":false}')

// onStatusUpdate eventData { \"encryption\": false, \"BTHome_version\": 2, \"pid\": 170, \"battery\": 89," script=1 ts=99481.285
// onStatusUpdate eventData \"illuminance\": 0, \"motion\": 1, \"rssi\": -59," script=1 ts=99481.286
// onStatusUpdate eventData \"address\": \"e8:e0:7e:a6:0c:6f\"" script=1 ts=99481.287
// onStatusUpdate eventData script=1 ts=99481.288
// onStatusUpdate eventData Info: \"New status update\"" script=1 ts=99481.288
// onStatusUpdate eventData Received message on topic:  groups/pool-house-lights message:  {\"op\":\"on\",\"keep\":false}" script=2 ts=99481.362
// onStatusUpdate eventData shelly_ejs_rpc.cpp:41   Shelly.call Switch.Set {\"id\":0,\"on\":true}" ts=99481.363
// onStatusUpdate eventData shelly_ejs_timer.cpp:43 Timer 0 handle not found" ts=99481.364
// onStatusUpdate eventData Turn on & auto-off" script=2 ts=99481.364
// onStatusUpdate eventData shos_rpc_inst.c:243     Switch.Set [5887@RPC.LOCAL] via loopback" ts=99481.391
// onStatusUpdate eventData shelly_notification:164 Status change of switch:0: {\"output\":true,\"source\":\"loopback\"}" ts=99481.392

// Motion on, then off

// onStatusUpdate eventData { \"encryption\": false, \"BTHome_version\": 2, \"pid\": 180, \"battery\": 89," script=1 ts=1111.908
// onStatusUpdate eventData \"illuminance\": 0, \"motion\": 1, \"rssi\": -58," script=1 ts=1111.909
// onStatusUpdate eventData \"address\": \"e8:e0:7e:a6:0c:6f\"" script=1 ts=1111.91
// onStatusUpdate eventData } script=1 ts=1111.91
// onStatusUpdate eventData Info: \"New status update\"" script=1 ts=1111.911

// onStatusUpdate eventData { \"encryption\": false, \"BTHome_version\": 2, \"pid\": 181, \"battery\": 89," script=1 ts=1174.131
// onStatusUpdate eventData \"illuminance\": 0, \"motion\": 0, \"rssi\": -59," script=1 ts=1174.132
// onStatusUpdate eventData \"address\": \"e8:e0:7e:a6:0c:6f\"" script=1 ts=1174.133
// onStatusUpdate eventData } script=1 ts=1174.133

// msg="\"name\": \"switch\"," script=1 ts=128.592 v=0
// msg="\"id\": 0, \"now\": 1747252166.87912797927," script=1 ts=128.594 v=0
// msg="\"info\": {" script=1 ts=128.594 v=0
// msg="\"component\": \"switch:0\"," script=1 ts=128.596 v=0
// msg="\"id\": 0," script=1 ts=128.597 v=0
// msg="\"event\": \"temperature_update\"," script=1 ts=128.597 v=0
// msg="\"temperature\": 54.9, \"range_min\": 2.9, \"ts\": 1747252166.87999987602 }" script=1 ts=128.598 v=0
// msg=} script=1 ts=128.599 v=0


// // Long Push (pool-house-1) w/ inverted-logic: true

// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"btn_down\",\"ts\":1747394072.52}" ts=140947.164
// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"long_push\",\"ts\":1747394073.52}" ts=140948.165
// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"btn_up\",\"ts\":1747394075.01}" ts=140949.654

// // Long push (lumiere-exterieure-droite) w/ inverted-logic: true
// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"btn_up\",\"ts\":1747394206.88}" ts=29445.847
// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"btn_down\",\"ts\":1747394208.82}" ts=29447.795
// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"long_push\",\"ts\":1747394209.83}" ts=29448.796

// // Time sync
// msg="shos_time.c:58          Setting time from SNTP (1747395389.708 delta -0.058)" ts=30628.746
// msg="shelly_sys.cpp:281      Time set to 30628.698585 from 1" ts=30628.748
// msg="shos_cron.c:223         And before: 1747519200 [2025/05/17 22:00:00 UTC]" ts=30628.75
// msg="shos_cron.c:223         Looking for next sunrise/sunset after: 1747395389 [2025/05/16 11:36:29 UTC]" ts=30628.749
// msg="shos_cron.c:223         And before: 1747432800 [2025/05/16 22:00:00 UTC]" ts=30628.749
// msg="shos_cron.c:223         Looking for next sunrise/sunset after: 1747432800 [2025/05/16 22:00:00 UTC]" ts=30628.749
// msg="shelly_notification:164 Status change of sys: {\"last_sync_ts\":1747395389}" ts=30628.793
// msg="shelly_notification:164 Status change of sys: {\"time\":\"13:36\",\"unixtime\":1747395389}" ts=30628.794

var ble_msgs = [{
  "addr":"6a:54:77:57:9f:3e",
  "rssi":-46,
  "addr_type":4,
  "advData":"02011a0303befe0dff0115101eecf33dfeaea8c43a",
  "flags":26,
  "service_uuids":["febe"],
  "manufacturer":"1501",
  "manufacturer_data":"101eecf33dfeaea8c43a"
},{
  "addr":"6a:54:77:57:9f:3e",
  "rssi":-44,
  "addr_type":4,
  "advData":"02011a0303befe0dff0115101eecf33dfeaea8c43a",
  "flags":26,
  "service_uuids":["febe"],
  "manufacturer":"1501",
  "manufacturer_data":"101eecf33dfeaea8c43a"
},{
  "addr":"6a:54:77:57:9f:3e",
  "rssi":-47,
  "addr_type":4,
  "advData":"02011a0303befe0dff0115101eecf33dfeaea8c43a",
  "flags":26,
  "service_uuids":["febe"],
  "manufacturer":"1501",
  "manufacturer_data":"101eecf33dfeaea8c43a"
},{
  "addr":"90:f1:57:ae:b0:04",
  "rssi":-92,
  "addr_type":1,
  "advData":"02010605ff87000cdb",
  "flags":6,
  "manufacturer":"0087",
  "manufacturer_data":"0cdb"
},{
  {"addr":"08:b6:1f:d9:d7:0a","rssi":-96,"addr_type":1,"advData":"02010610ffa90b0105000b00100a08d7d91fb608","flags":6,"manufacturer":"0ba9","manufacturer_data":"0105000b00100a08d7d91fb608"}
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312683 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"6b:0c:3f:4d:5c:8a\",\"rssi\":-89,\"addr_type\":4,\"advData\":\"02011a020a0c0bff4c001006361e44776266\",\"flags\":26,\"man" script=3 ts=52962.145 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312684 device=shellyplus1-b8d61a85ed58 msg="ufacturer\":\"004c\",\"manufacturer_data\":\"1006361e44776266\"}" script=3 ts=52962.146 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312685 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"e0:05:02:68:49:d7\",\"rssi\":-74,\"addr_type\":2,\"advData\":\"1eff4c00121910dff0717a22fe09a14800e7ddbf3ab67b7897da1" script=3 ts=52962.146 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312686 device=shellyplus1-b8d61a85ed58 msg="170be01b2\",\"manufacturer\":\"004c\",\"manufacturer_data\":\"121910dff0717a22fe09a14800e7ddbf3ab67b7897da1170be01b2\"}" script=3 ts=52962.147 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312687 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"4c:4e:65:42:47:ab\",\"rssi\":-74,\"addr_type\":4,\"advData\":\"02011a17ff4c0009081304c0a801371b58160800eb7d17c663176" script=3 ts=52962.542 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312688 device=shellyplus1-b8d61a85ed58 msg="b\",\"flags\":26,\"manufacturer\":\"004c\",\"manufacturer_data\":\"09081304c0a801371b58160800eb7d17c663176b\"}" script=3 ts=52962.545 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312689 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"08:b6:1f:d9:33:3e\",\"rssi\":-88,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100a3c33d91fb608\",\"flags\":6,\"" script=3 ts=52962.547 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312690 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100a3c33d91fb608\"}" script=3 ts=52962.548 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312691 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"84:fc:e6:3b:f4:66\",\"rssi\":-91,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b15100a64f43be6fc84\",\"flags\":6,\"" script=3 ts=52962.549 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312692 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b15100a64f43be6fc84\"}" script=3 ts=52962.549 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312693 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"08:b6:1f:d8:b8:9e\",\"rssi\":-86,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100a9cb8d81fb608\",\"flags\":6,\"" script=3 ts=52963.154 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312694 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100a9cb8d81fb608\"}" script=3 ts=52963.154 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312695 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"48:55:19:9c:98:8a\",\"rssi\":-58,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100a88989c195548\",\"flags\":6,\"" script=3 ts=52963.159 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312696 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100a88989c195548\"}" script=3 ts=52963.159 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312697 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"b8:d6:1a:85:a9:72\",\"rssi\":-78,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100a70a9851ad6b8\",\"flags\":6,\"" script=3 ts=52963.159 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312698 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100a70a9851ad6b8\"}" script=3 ts=52963.16 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312699 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"b0:81:84:a5:3f:26\",\"rssi\":-96,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b05180a243fa58481b0\",\"flags\":6,\"" script=3 ts=52963.16 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312700 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b05180a243fa58481b0\"}" script=3 ts=52963.16 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312701 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"90:f1:57:ae:b0:04\",\"rssi\":-90,\"addr_type\":1,\"advData\":\"02010605ff87000cdb\",\"flags\":6,\"manufacturer\":\"0087\",\"" script=3 ts=52963.739 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312702 device=shellyplus1-b8d61a85ed58 msg="manufacturer_data\":\"0cdb\"}" script=3 ts=52963.744 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312703 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"a0:dd:6c:a1:c5:8a\",\"rssi\":-96,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b04200a88c5a16cdda0\",\"flags\":6,\"" script=3 ts=52963.747 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312704 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b04200a88c5a16cdda0\"}" script=3 ts=52963.747 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312705 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"b8:d6:1a:85:a8:e2\",\"rssi\":-50,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100ae0a8851ad6b8\",\"flags\":6,\"" script=3 ts=52963.747 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312706 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100ae0a8851ad6b8\"}" script=3 ts=52963.747 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312707 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"47:83:29:e1:05:7e\",\"rssi\":-84,\"addr_type\":4,\"advData\":\"02011a020a090aff4c0010050b18c93534\",\"flags\":26,\"manuf" script=3 ts=52963.748 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312708 device=shellyplus1-b8d61a85ed58 msg="acturer\":\"004c\",\"manufacturer_data\":\"10050b18c93534\"}" script=3 ts=52963.748 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312709 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"52:45:aa:f0:ac:44\",\"rssi\":-97,\"addr_type\":4,\"advData\":\"02011a0dff4c00160800d2c1459762" script=3 ts=52964.456 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312710 device=shellyplus1-b8d61a85ed58 msg="acturer\":\"004c\",\"manufacturer_data\":\"160800d2c14597620938\"}" script=3 ts=52964.459 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312711 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"73:1e:95:f3:71:59\",\"rssi\":-47,\"addr_type\":4,\"advData\":\"02011a0303befe0dff0115101eecf33dfeaea8c43a\",\"flags\":2" script=3 ts=52964.864 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312712 device=shellyplus1-b8d61a85ed58 msg="6,\"service_uuids\":[\"febe\"],\"manufacturer\":\"1501\",\"manufacturer_data\":\"101eecf33dfeaea8c43a\"}" script=3 ts=52964.865 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312713 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"fa:9a:dc:e3:41:da\",\"rssi\":-69,\"addr_type\":2,\"advData\":\"1eff4c001219aa71670bcb98de9acdeb3e22fbd3775e76ab46961" script=3 ts=52964.866 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312714 device=shellyplus1-b8d61a85ed58 msg="e49530263\",\"manufacturer\":\"004c\",\"manufacturer_data\":\"1219aa71670bcb98de9acdeb3e22fbd3775e76ab46961e49530263\"}" script=3 ts=52964.866 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312715 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"54:32:04:64:19:fa\",\"rssi\":-74,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b15100af81964043254\",\"flags\":6,\"" script=3 ts=52965.224 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312716 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b15100af81964043254\"}" script=3 ts=52965.227 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312717 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"56:53:69:c4:68:81\",\"rssi\":-78,\"addr_type\":4,\"advData\":\"02011a0dff4c001608005948617a31e8a9\",\"flags\":26,\"manuf" script=3 ts=52965.228 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312718 device=shellyplus1-b8d61a85ed58 msg="acturer\":\"004c\",\"manufacturer_data\":\"1608005948617a31e8a9\"}" script=3 ts=52965.229 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312719 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"54:32:04:52:2c:b6\",\"rssi\":-89,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b15100ab42c52043254\",\"flags\":6,\"" script=3 ts=52965.23 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312720 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b15100ab42c52043254\"}" script=3 ts=52965.231 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312721 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"b8:d6:1a:86:ca:c2\",\"rssi\":-94,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100ac0ca861ad6b8\",\"flags\":6,\"" script=3 ts=52965.86 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312722 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100ac0ca861ad6b8\"}" script=3 ts=52965.864 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312723 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"54:32:04:40:d0:2e\",\"rssi\":-95,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b15100a2cd040043254\",\"flags\":6,\"" script=3 ts=52965.865 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312724 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b15100a2cd040043254\"}" script=3 ts=52965.867 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312725 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"56:11:e1:94:5c:c8\",\"rssi\":-94,\"addr_type\":4,\"advData\":\"02011a020a0c0cff4c001007301ff2d7174738\",\"flags\":26,\"m" script=3 ts=52965.868 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312726 device=shellyplus1-b8d61a85ed58 msg="anufacturer\":\"004c\",\"manufacturer_data\":\"1007301ff2d7174738\"}" script=3 ts=52965.869 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312727 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"4c:4e:65:42:47:ab\",\"rssi\":-78,\"addr_type\":4,\"advData\":\"02011a17ff4c0009081304c0a801371b58160800eb7d17c663176" script=3 ts=52965.869 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312728 device=shellyplus1-b8d61a85ed58 msg="b\",\"flags\":26,\"manufacturer\":\"004c\",\"manufacturer_data\":\"09081304c0a801371b58160800eb7d17c663176b\"}" script=3 ts=52965.87 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312729 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"08:b6:1f:d1:41:ea\",\"rssi\":-87,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100ae841d11fb608\",\"flags\":6,\"" script=3 ts=52966.712 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312730 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100ae841d11fb608\"}" script=3 ts=52966.717 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312731 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"6b:0c:3f:4d:5c:8a\",\"rssi\":-92,\"addr_type\":4,\"advData\":\"02011a020a0c0bff4c001006361e44776266\",\"flags\":26,\"man" script=3 ts=52966.718 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312732 device=shellyplus1-b8d61a85ed58 msg="ufacturer\":\"004c\",\"manufacturer_data\":\"1006361e44776266\"}" script=3 ts=52966.718 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312733 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"54:32:04:40:f8:7a\",\"rssi\":-96,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b15100a78f840043254\",\"flags\":6,\"" script=3 ts=52966.719 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312734 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b15100a78f840043254\"}" script=3 ts=52966.719 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312735 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"48:55:19:9d:b0:66\",\"rssi\":-78,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100a64b09d195548\",\"flags\":6,\"" script=3 ts=52966.719 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312736 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100a64b09d195548\"}" script=3 ts=52966.72 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312737 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"b8:d6:1a:85:a8:e2\",\"rssi\":-54,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100ae0a8851ad6b8\",\"flags\":6,\"" script=3 ts=52967.404 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312738 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100ae0a8851ad6b8\"}" script=3 ts=52967.405 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312739 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"b0:81:84:a5:3f:26\",\"rssi\":-92,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b05180a243fa58481b0\",\"flags\":6,\"" script=3 ts=52967.406 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312740 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b05180a243fa58481b0\"}" script=3 ts=52967.407 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312741 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"84:fc:e6:3b:f4:66\",\"rssi\":-90,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b15100a64f43be6fc84\",\"flags\":6,\"" script=3 ts=52967.407 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312742 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b15100a64f43be6fc84\"}" script=3 ts=52967.408 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312743 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"08:b6:1f:d9:33:3e\",\"rssi\":-82,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100a3c33d91fb608\",\"flags\":6,\"" script=3 ts=52967.409 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312744 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100a3c33d91fb608\"}" script=3 ts=52967.409 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312745 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"ed:5f:c2:0a:06:86\",\"rssi\":-94,\"addr_type\":2,\"advData\":\"07ff4c0012020003\",\"manufacturer\":\"004c\",\"manufacturer" script=3 ts=52967.721 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312746 device=shellyplus1-b8d61a85ed58 msg="_data\":\"12020003\"}" script=3 ts=52967.723 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312747 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"47:83:29:e1:05:7e\",\"rssi\":-94,\"addr_type\":4,\"advData\":\"02011a020a090aff4c0010050b18c93534\",\"flags\":26,\"manuf" script=3 ts=52967.724 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312748 device=shellyplus1-b8d61a85ed58 msg="acturer\":\"004c\",\"manufacturer_data\":\"10050b18c93534\"}" script=3 ts=52967.724 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312749 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"d3:26:37:c2:77:b6\",\"rssi\":-77,\"addr_type\":2,\"advData\":\"07ff4c0012020003\",\"manufacturer\":\"004c\",\"manufacturer" script=3 ts=52967.724 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312750 device=shellyplus1-b8d61a85ed58 msg="_data\":\"12020003\"}" script=3 ts=52967.724 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312751 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"08:b6:1f:d9:d7:0a\",\"rssi\":-90,\"addr_type\":1,\"advData\":\"02010610ffa90b0105000b00100a08d7d91fb608\",\"flags\":6,\"" script=3 ts=52967.856 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312752 device=shellyplus1-b8d61a85ed58 msg="manufacturer\":\"0ba9\",\"manufacturer_data\":\"0105000b00100a08d7d91fb608\"}" script=3 ts=52967.856 v=0
  7:46PM INF shelly/script/debug.go:280 > UDP-logger component=1 count=312753 device=shellyplus1-b8d61a85ed58 msg="shos_mqtt_conn.c:965    MQTT0 queue overflow!" ts=52967.859 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312754 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"56:53:69:c4:68:81\",\"rssi\":-72,\"addr_type\":4,\"advData\":\"02011a0dff4c001608005948617a31e8a9\",\"flags\":26,\"manuf" script=3 ts=52968.321 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312755 device=shellyplus1-b8d61a85ed58 msg="acturer\":\"004c\",\"manufacturer_data\":\"1608005948617a31e8a9\"}" script=3 ts=52968.321 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312756 device=shellyplus1-b8d61a85ed58 msg="[BLE-MQTT] {\"addr\":\"73:1e:95:f3:71:59\",\"rssi\":-48,\"addr_type\":4,\"advData\":\"02011a0303befe0dff0115101eecf33dfeaea8c43a\",\"flags\":2" script=3 ts=52968.547 v=0
  7:46PM INF shelly/script/debug.go:278 > UDP-logger count=312757 device=shellyplus1-b8d61a85ed58 msg="6,\"service_uuids\":[\"febe\"],\"manufacturer\":\"1501\",\"manufacturer_data\":\"101eecf33dfeaea8c43a\"}" script=3 ts=52968.551 v=0

}]
