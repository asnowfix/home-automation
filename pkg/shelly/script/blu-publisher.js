/**
 * This script will use BLE observer to listen for advertising data from nearby Shelly BLU devices,
 * decodes the data using a BTHome data structure, and publishes the decoded data over MQTT.
 *
 * This script DOESN'T execute actions, only publishes events. Can be used in conjunction with
 * `ble-events-handler.js` example. By default, it publishes under the topic `shelly-blu/events/<MAC>`,
 * but you can configure this via the `ble.mqtt.topic` configuration variable. The body of the MQTT
 * message contains all the data parsed from the BLE device.
 *
 * Represents data provided by each device.
 * Every value illustrating a sensor reading (e.g., button) may be a singular sensor value or
 * an array of values if the object has multiple instances.
 * @typedef {Object} DeviceData
 * @property {number} pid - Packet ID.
 * @property {number} battery - The battery level of the device in percentage (%).
 * @property {number} rssi - The signal strength in decibels (dB).
 * @property {string} address - The MAC address of the Shelly BLU device.
 * @property {number | number[]} [temperature] - The temperature value in degrees Celsius if the device has a temperature sensor. (Can be an array if has multiple instances)
 * @property {number | number[]} [humidity] - The humidity value in percentage (%) if the device has a humidity sensor. (Can be an array if has multiple instances)
 * @property {number | number[]} [illuminance] - The illuminance value in lux if the device has a light sensor. (Can be an array if has multiple instances)
 * @property {number | number[]} [motion] - Motion status: 0 for clear, 1 for motion (if the device has a motion sensor). (Can be an array if has multiple instances)
 * @property {number | number[]} [window] - Window status: 0 for closed, 1 for open (if the device has a reed switch). (Can be an array if has multiple instances)
 * @property {number | number[]} [button] - The number of presses if the device has a button. (Can be an array if has multiple instances)
 * @property {number | number[]} [rotation] - The angle of rotation in degrees if the device has a gyroscope. (Can be an array if has multiple instances)
 *
 * @example
 * {"component":"script:*","name":"script","id":*,"now":*,"info":{"component":"script:*","id":*,"event":"shelly-blu","data":{"encryption":false,"BTHome_version":2,"pid":118,"battery":100,"button":1,"rssi":-76,"address":*},"ts":*}}
 */

/******************* START CHANGE HERE *******************/
const CONFIG = {
  script: "[blu-publisher] ",

  // Specify the destination event where the decoded BLE data will be emitted. It allows for easy identification by other applications/scripts
  eventName: "shelly-blu",

  kvsPrefix: "follow/shelly-blu/",
  refreshMs: 60 * 1000, // periodically refresh followed MACs

  // If the script owns the scanner and this value is set to true, the scan will be active.
  // If the script does not own the scanner, it may remain passive even when set to true.
  // Active scan means the scanner will ping back the Bluetooth device to receive all its data, but it will drain the battery faster
  active: false,

  log: true
};

var STATE = {
  // mac (lowercase) => { switchIdStr: string, switchIndex: number }
  follows: {},
};

function log() {
  if (!CONFIG.log) return;
  var s = "";
  for (var i = 0; i < arguments.length; i++) {
    try {
      var a = arguments[i];
      if (typeof a === "object") {
        s += JSON.stringify(a);
      } else {
        s += String(a);
      }
    } catch (e) {
      s += String(arguments[i]);
      // Ensure 'e' is referenced so the minifier doesn't drop it and produce `catch {}`
      if (e && false) {}
    }
    if (i + 1 < arguments.length) s += " ";
  }
  print(CONFIG.script, s);
}

function normalizeMac(mac) {
  if (!mac) return "";
  return String(mac).toLowerCase();
}

function parseSwitchIndex(switchIdStr) {
  // Expecting format "switch:<number>"
  if (typeof switchIdStr !== "string") return null;
  var parts = switchIdStr.split(":");
  if (parts.length !== 2) return null;
  if (parts[0] !== "switch") return null;
  var n = Number(parts[1]);
  if (isNaN(n)) return null;
  return n;
}

function loadFollowsFromKVS(callback) {
  // Refresh STATE.follows from KVS
  Shelly.call("KVS.List", { prefix: CONFIG.kvsPrefix }, function (resp, err) {
    if (err) {
      log("KVS.List error:", err);
      if (callback) callback(false);
      return;
    }
    // Normalize possible response shapes:
    // - resp.keys: ["key1", "key2", ...]
    // - resp.keys: { "key1": true, ... } (object map)
    // - resp.items: [{ key: "key1" }, ...]
    var list = [];
    if (resp) {
      if (resp.keys) {
        if (Array.isArray(resp.keys)) {
          list = resp.keys;
        } else if (typeof resp.keys === "object") {
          list = Object.keys(resp.keys);
        }
      } else if (Array.isArray(resp.items)) {
        for (var li = 0; li < resp.items.length; li++) {
          var it = resp.items[li];
          if (it && it.key) list.push(it.key);
        }
      }
    }
    log("KVS.List keys:", list.length);
    var newMap = {};
    if (!list || !list.length) {
      STATE.follows = newMap;
      log("No followed MACs.");
      if (callback) callback(true);
      return;
    }

    var pending = list.length;
    for (var i = 0; i < list.length; i++) {
      (function (k) {
        Shelly.call("KVS.Get", { key: k }, function (gresp, gerr) {
          if (gerr) {
            log("KVS.Get error for", k, ":", gerr);
          } else if (gresp && typeof gresp.value === "string") {
            try {
              var value = JSON.parse(gresp.value);
              var switchIdStr = value && value.switch_id ? String(value.switch_id) : null;
              var mac = k.substr(CONFIG.kvsPrefix.length);
              mac = normalizeMac(mac);
              var idx = parseSwitchIndex(switchIdStr);
              if (mac && idx !== null) {
                newMap[mac] = {
                  switchIdStr: switchIdStr,
                  switchIndex: idx,
                };
              } else {
                log("Ignoring invalid follow entry:", k, gresp.value);
              }
            } catch (e) {
              log("JSON parse error for", k, e);
            }
          } else {
            log("KVS.Get error for", k, gerr);
          }
          pending--;
          if (pending === 0) {
            STATE.follows = newMap;
            log("Loaded follows:", newMap);
            if (callback) callback(true);
          }
        });
      })(list[i]);
    }
  });
}

/******************* STOP CHANGE HERE *******************/

const BTHOME_SVC_ID_STR = "fcd2";

const uint8 = 0;
const int8 = 1;
const uint16 = 2;
const int16 = 3;
const uint24 = 4;
const int24 = 5;

// The BTH object defines the structure of the BTHome data
const BTH = {
  0x00: { n: "pid", t: uint8 },
  0x01: { n: "battery", t: uint8, u: "%" },
  0x02: { n: "temperature", t: int16, f: 0.01, u: "tC" },
  0x03: { n: "humidity", t: uint16, f: 0.01, u: "%" },
  0x05: { n: "illuminance", t: uint24, f: 0.01 },
  0x21: { n: "motion", t: uint8 },
  0x2d: { n: "window", t: uint8 },
  0x2e: { n: "humidity", t: uint8, u: "%" },
  0x3a: { n: "button", t: uint8 },
  0x3f: { n: "rotation", t: int16, f: 0.1 },
  0x45: { n: "temperature", t: int16, f: 0.1, u: "tC" },
};

function getByteSize(type) {
  if (type === uint8 || type === int8) return 1;
  if (type === uint16 || type === int16) return 2;
  if (type === uint24 || type === int24) return 3;
  //impossible as advertisements are much smaller;
  return 255;
}

// functions for decoding and unpacking the service data from Shelly BLU devices
const BTHomeDecoder = {
  utoi: function (num, bitsz) {
    const mask = 1 << (bitsz - 1);
    return num & mask ? num - (1 << bitsz) : num;
  },
  getUInt8: function (buffer) {
    return buffer.at(0);
  },
  getInt8: function (buffer) {
    return this.utoi(this.getUInt8(buffer), 8);
  },
  getUInt16LE: function (buffer) {
    return 0xffff & ((buffer.at(1) << 8) | buffer.at(0));
  },
  getInt16LE: function (buffer) {
    return this.utoi(this.getUInt16LE(buffer), 16);
  },
  getUInt24LE: function (buffer) {
    return (
      0x00ffffff & ((buffer.at(2) << 16) | (buffer.at(1) << 8) | buffer.at(0))
    );
  },
  getInt24LE: function (buffer) {
    return this.utoi(this.getUInt24LE(buffer), 24);
  },
  getBufValue: function (type, buffer) {
    if (buffer.length < getByteSize(type)) return null;
    let res = null;
    if (type === uint8) res = this.getUInt8(buffer);
    if (type === int8) res = this.getInt8(buffer);
    if (type === uint16) res = this.getUInt16LE(buffer);
    if (type === int16) res = this.getInt16LE(buffer);
    if (type === uint24) res = this.getUInt24LE(buffer);
    if (type === int24) res = this.getInt24LE(buffer);
    return res;
  },

  // Unpacks the service data buffer from a Shelly BLU device
  unpack: function (buffer) {
    //beacons might not provide BTH service data
    if (typeof buffer !== "string" || buffer.length === 0) return null;
    let result = {};
    let _dib = buffer.at(0);
    result["encryption"] = _dib & 0x1 ? true : false;
    result["BTHome_version"] = _dib >> 5;
    if (result["BTHome_version"] !== 2) return null;
    //can not handle encrypted data
    if (result["encryption"]) return result;
    buffer = buffer.slice(1);

    let _bth;
    let _value;
    while (buffer.length > 0) {
      _bth = BTH[buffer.at(0)];
      if (typeof _bth === "undefined") {
        log("BTH: Unknown type");
        break;
      }
      buffer = buffer.slice(1);
      _value = this.getBufValue(_bth.t, buffer);
      if (_value === null) break;
      if (typeof _bth.f !== "undefined") _value = _value * _bth.f;

      if (typeof result[_bth.n] === "undefined") {
        result[_bth.n] = _value;
      }
      else {
        if (Array.isArray(result[_bth.n])) {
          result[_bth.n].push(_value);
        }
        else {
          result[_bth.n] = [
            result[_bth.n],
            _value
          ];
        }
      }

      buffer = buffer.slice(getByteSize(_bth.t));
    }
    return result;
  },
};

/**
 * Еmitting the decoded BLE data to a specified event. It allows other scripts to receive and process the emitted data
 * @param {DeviceData} data
 */
function emitData(data) {
  if (typeof data !== "object") {
    return;
  }

  if (MQTT.isConnected()) {
    topic = CONFIG.eventName + "/events/" + data.address;
    log("Publishing event via MQTT on topic: ", topic, "data:", data);
    MQTT.publish(topic, JSON.stringify(data), 1, false);
  }

  try {
    var follow = STATE.follows[data.mac];
    if (!follow) return; // not followed

    log("Emitting local event data: ", data);
    Shelly.emitEvent(CONFIG.eventName, data);
  } catch (e) {
    log("Error emitting local event: ", e);
  }
}

//saving the id of the last packet, this is used to filter the duplicated packets
let lastPacketId = 0x100;

// Callback for the BLE scanner object
function BLEScanCallback(event, result) {
  //exit if not a result of a scan
  if (event !== BLE.Scanner.SCAN_RESULT) {
    return;
  }

  //exit if service_data member is missing
  if (
    typeof result.service_data === "undefined" ||
    typeof result.service_data[BTHOME_SVC_ID_STR] === "undefined"
  ) {
    return;
  }

  let unpackedData = BTHomeDecoder.unpack(
    result.service_data[BTHOME_SVC_ID_STR]
  );

  //exit if unpacked data is null or the device is encrypted
  if (
    unpackedData === null ||
    typeof unpackedData === "undefined" ||
    unpackedData["encryption"]
  ) {
    log("Error: Encrypted devices are not supported");
    return;
  }

  //exit if the event is duplicated
  if (lastPacketId === unpackedData.pid) {
    return;
  }

  lastPacketId = unpackedData.pid;

  unpackedData.rssi = result.rssi;
  unpackedData.address = result.addr;

  emitData(unpackedData);
}

// Initializes the script and performs the necessary checks and configurations
function initBLEScanner() {
  //exit if can't find the config
  if (typeof CONFIG === "undefined") {
    log("Error: Undefined config");
    return;
  }

  //get the config of ble component
  const BLEConfig = Shelly.getComponentConfig("ble");

  //exit if the BLE isn't enabled
  if (!BLEConfig.enable) {
    log("Error: The Bluetooth is not enabled, please enable it from settings");
    return;
  }

  //check if the scanner is already running
  if (BLE.Scanner.isRunning()) {
    log("Info: The BLE gateway is running, the BLE scan configuration is managed by the device");
  }
  else {
    //start the scanner
    const bleScanner = BLE.Scanner.Start({
      duration_ms: BLE.Scanner.INFINITE_SCAN,
      active: CONFIG.active
    });

    if (!bleScanner) {
      log("Error: Can not start new scanner");
    }
  }

  //subscribe a callback to BLE scanner
  BLE.Scanner.Subscribe(BLEScanCallback);
}

function scheduleRefresh() {
  Timer.set(CONFIG.refreshMs, true, function () {
    loadFollowsFromKVS();
  });
}

// Init
loadFollowsFromKVS(function () {
  scheduleRefresh();
});
initBLEScanner();
