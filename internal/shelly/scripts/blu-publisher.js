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
 * 
 * Supports all BTHome v2 object IDs as defined in https://bthome.io/format/
 * 
 * @typedef {Object} DeviceData
 * @property {boolean} encryption - Whether the device uses encryption
 * @property {number} BTHome_version - BTHome protocol version (should be 2)
 * @property {number} pid - Packet ID (0x00)
 * @property {number} rssi - The signal strength in decibels (dB)
 * @property {string} address - The MAC address of the Shelly BLU device
 * 
 * Power & Energy:
 * @property {number | number[]} [battery] - Battery level in % (0x01)
 * @property {number | number[]} [energy] - Energy in kWh (0x0a)
 * @property {number | number[]} [power] - Power in W (0x0b)
 * @property {number | number[]} [voltage] - Voltage in V (0x0c)
 * @property {number | number[]} [current] - Current in A (0x43)
 * 
 * Environmental Sensors:
 * @property {number | number[]} [temperature] - Temperature in °C (0x02/0x45)
 * @property {number | number[]} [humidity] - Humidity in % (0x03/0x2e)
 * @property {number | number[]} [pressure] - Pressure in hPa (0x04)
 * @property {number | number[]} [illuminance] - Illuminance in lux (0x05)
 * @property {number | number[]} [mass] - Mass in kg (0x06)
 * @property {number | number[]} [dew_point] - Dew point in °C (0x08)
 * 
 * Motion & Position:
 * @property {number | number[]} [motion] - Motion (0=clear, 1=detected) (0x21)
 * @property {number | number[]} [window] - Window (0=closed, 1=open) (0x2d)
 * @property {number | number[]} [button] - Button press count (0x3a)
 * @property {number | number[]} [rotation] - Rotation in degrees (0x3f)
 * 
 * Distance:
 * @property {number | number[]} [distance_mm] - Distance in mm (0x40)
 * @property {number | number[]} [distance_m] - Distance in m (0x41)
 * 
 * Timestamp:
 * @property {number | number[]} [timestamp] - Unix timestamp in seconds (0x50)
 * 
 * Acceleration:
 * @property {number | number[]} [acceleration] - Acceleration in m/s² (0x51)
 * 
 * Variable-length data:
 * @property {string | string[]} [text] - Text data (0x53)
 * @property {string | string[]} [raw] - Raw data as hex string (0x54)
 * 
 * Raw BTHome frame:
 * @property {Object} [bthome] - Raw BTHome frame data from BLE advertisement
 * @property {Object} [bthome.service_data] - Service data from BLE advertisement (UUID: service data hex string)
 * @property {Object} [bthome.manufacturer_data] - Manufacturer data from BLE advertisement (if present)
 * @property {string} [bthome.local_name] - Local name from BLE advertisement (if present)
 *
 * @example
 * {"component":"script:*","name":"script","id":*,"now":*,"info":{"component":"script:*","id":*,"event":"shelly-blu","data":{"encryption":false,"BTHome_version":2,"pid":118,"battery":100,"button":1,"rssi":-76,"address":*,"bthome":{"service_data":{"fcd2":"40..."},"local_name":"SBBT-002C"}},"ts":*}}
 */

// === STATIC CONSTANTS ===
const SCRIPT_NAME = "blu-publisher";
const CONFIG_KEY_PREFIX = 'script/' + SCRIPT_NAME + '/';
const SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";

const CONFIG = {
  enableLogging: true,

  // Specify the destination event where the decoded BLE data will be emitted. It allows for easy identification by other applications/scripts
  eventName: "shelly-blu",

  kvsPrefix: "follow/shelly-blu/",

  // If the script owns the scanner and this value is set to true, the scan will be active.
  // If the script does not own the scanner, it may remain passive even when set to true.
  // Active scan means the scanner will ping back the Bluetooth device to receive all its data, but it will drain the battery faster
  active: false,
};

var STATE = {
  // In-memory cache of follows loaded from KVS by loadFollowsFromKVS()
  // KVS keys are set externally via "myhome ctl follow blu" command
  // Each followed MAC has its own KVS key: follow/shelly-blu/<mac>
  // Empty map = publish ALL BLU events (no filtering)
  follows: {}
};

// Polyfill for print function
if (typeof print !== "function") {
  print = console.log;
}

/**
 * Logs a message if logging is enabled.
 * @returns void
 */
function log() {
  if (!CONFIG.enableLogging) return;
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
  print(SCRIPT_PREFIX, s);
}

function getFollows() {
  return STATE.follows;
}

function setFollows(map) {
  STATE.follows = map || {};
}

/**
 * In-memory follows cache populated from KVS
 * Stores a map of followed BLE MACs to local switch control info.
 *
 * @typedef {Object.<string, FollowEntry>} FollowsMap
 * @typedef {Object} FollowEntry
 * @property {string} switchIdStr // e.g. "switch:0"
 * @property {number} switchIndex // numeric index parsed from switchIdStr
 */

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

function onKvsGetResponse(k, newMap, onComplete, gresp, gerr) {
  if (gerr) {
    log("KVS.Get error for", k, ":", gerr);
    onComplete();
    return;
  }
  if (!gresp || typeof gresp.value !== "string") {
    log("KVS.Get error for", k, gerr);
    onComplete();
    return;
  }
  
  // Skip keys that don't start with our prefix
  if (k.indexOf(CONFIG.kvsPrefix) !== 0) {
    log("Skipping non-follow key:", k);
    onComplete();
    return;
  }
  
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
  onComplete();
}

function processKvsKey(k, newMap, onComplete) {
  Shelly.call("KVS.Get", { key: k }, function(gresp, gerr) {
    onKvsGetResponse(k, newMap, onComplete, gresp, gerr);
  });
}

function onAllKeysProcessed(newMap, callback) {
  setFollows(newMap);
  var followCount = Object.keys(newMap).length;
  if (followCount === 0) {
    log("Loaded follows: empty - publish-all mode enabled");
  } else {
    log("Loaded follows:", followCount, "MACs - filtering mode enabled");
  }
  if (callback) callback(true);
}

function processKeysSequentially(list, newMap, callback, index) {
  if (index >= list.length) {
    // All keys processed
    onAllKeysProcessed(newMap, callback);
    return;
  }
  
  // Process one key at a time
  processKvsKey(list[index], newMap, function() {
    processKeysSequentially(list, newMap, callback, index + 1);
  });
}

function onKvsListResponse(callback, resp, err) {
  if (err) {
    log("KVS.List error:", err);
    if (callback) callback(false);
    return;
  }
  
  // Normalize possible response shapes
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
    setFollows(newMap);
    log("No followed MACs - publish-all mode enabled");
    if (callback) callback(true);
    return;
  }

  // Process keys sequentially to avoid "too many calls in progress"
  processKeysSequentially(list, newMap, callback, 0);
}

function loadFollowsFromKVS(callback) {
  Shelly.call("KVS.List", { prefix: CONFIG.kvsPrefix }, function(resp, err) {
    onKvsListResponse(callback, resp, err);
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
const uint32 = 6;
const int32 = 7;

// The BTH object defines the structure of the BTHome data
// Based on BTHome v2 specification documented in <https://bthome.io/format/>
// All object IDs are included for complete BTHome v2 support
const BTH = {
  // Packet ID
  0x00: { n: "pid", t: uint8 },
  
  // Power & Energy
  0x01: { n: "battery", t: uint8, u: "%" },
  0x0a: { n: "energy", t: uint24, f: 0.001, u: "kWh" },
  0x0b: { n: "power", t: uint24, f: 0.01, u: "W" },
  0x0c: { n: "voltage", t: uint16, f: 0.001, u: "V" },
  0x43: { n: "current", t: uint16, f: 0.001, u: "A" },
  
  // Environmental Sensors
  0x02: { n: "temperature", t: int16, f: 0.01, u: "tC" },
  0x03: { n: "humidity", t: uint16, f: 0.01, u: "%" },
  0x04: { n: "pressure", t: uint24, f: 0.01, u: "hPa" },
  0x05: { n: "illuminance", t: uint24, f: 0.01, u: "lux" },
  0x06: { n: "mass", t: uint16, f: 0.01, u: "kg" },
  0x08: { n: "dew_point", t: int16, f: 0.01, u: "tC" },
  0x2e: { n: "humidity", t: uint8, u: "%" },
  0x45: { n: "temperature", t: int16, f: 0.1, u: "tC" },
  
  // Motion & Position
  0x21: { n: "motion", t: uint8 },
  0x2d: { n: "window", t: uint8 },
  0x3a: { n: "button", t: uint8 },
  0x3f: { n: "rotation", t: uint16, f: 0.1, u: "degrees" },
  
  // Distance
  0x40: { n: "distance_mm", t: uint16, u: "mm" },
  0x41: { n: "distance_m", t: uint16, f: 0.1, u: "m" },
  
  // Timestamp
  0x50: { n: "timestamp", t: uint32, u: "s" },
  
  // Acceleration
  0x51: { n: "acceleration", t: uint16, f: 0.001, u: "m/s2" },
  
  // Variable length sensors (text and raw) are not included here
  // as they require special handling with length byte
  // 0x53: text (variable length)
  // 0x54: raw (variable length)
};

function getByteSize(type) {
  if (type === uint8 || type === int8) return 1;
  if (type === uint16 || type === int16) return 2;
  if (type === uint24 || type === int24) return 3;
  if (type === uint32 || type === int32) return 4;
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
  getUInt32LE: function (buffer) {
    return (
      0xffffffff & ((buffer.at(3) << 24) | (buffer.at(2) << 16) | (buffer.at(1) << 8) | buffer.at(0))
    );
  },
  getInt32LE: function (buffer) {
    return this.utoi(this.getUInt32LE(buffer), 32);
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
    if (type === uint32) res = this.getUInt32LE(buffer);
    if (type === int32) res = this.getInt32LE(buffer);
    return res;
  },

  // Decode variable-length text data (0x53)
  // Format: [length_byte][utf8_text_bytes]
  getTextValue: function (buffer) {
    if (buffer.length < 1) return null;
    var length = buffer.at(0);
    if (buffer.length < 1 + length) return null;
    
    // Extract text bytes and convert to string
    var text = "";
    for (var i = 1; i <= length; i++) {
      text += String.fromCharCode(buffer.at(i));
    }
    return text;
  },

  // Decode variable-length raw data (0x54)
  // Format: [length_byte][raw_bytes]
  // Returns hex string representation
  getRawValue: function (buffer) {
    if (buffer.length < 1) return null;
    var length = buffer.at(0);
    if (buffer.length < 1 + length) return null;
    
    // Convert raw bytes to hex string
    var hex = "";
    for (var i = 1; i <= length; i++) {
      var byte = buffer.at(i);
      var h = byte.toString(16);
      if (h.length < 2) h = "0" + h;
      hex += h;
    }
    return hex;
  },

  // Get the size of variable-length data (including length byte)
  getVariableLengthSize: function (buffer) {
    if (buffer.length < 1) return 0;
    return 1 + buffer.at(0); // length byte + data bytes
  },

  // Unpacks the service data buffer from a Shelly BLU device
  unpack: function (buffer) {
    //beacons might not provide BTH service data
    if (typeof buffer !== "string" || buffer.length === 0) return null;
    let result = {};
    let _dib = buffer.at(0);
    result["encryption"] = _dib & 0x1 ? true : false;
    result["BTHome_version"] = _dib >> 5;
    if (result["BTHome_version"] !== 2) {
      log("BTHome: unknown version", result["BTHome_version"]);
      return null;
    }
    if (result["encryption"]) {
      log("BTHome: return only skipping encrypted data");
      return result;
    }
    buffer = buffer.slice(1);

    let _bth;
    let _value;
    let _objectId;
    let _bytesToSkip;
    while (buffer.length > 0) {
      _objectId = buffer.at(0);
      _bth = BTH[_objectId];
      
      // Handle variable-length sensors (text and raw)
      if (_objectId === 0x53) {
        // Text sensor
        buffer = buffer.slice(1);
        _value = this.getTextValue(buffer);
        if (_value === null) break;
        _bytesToSkip = this.getVariableLengthSize(buffer);
        
        if (typeof result["text"] === "undefined") {
          result["text"] = _value;
        } else {
          if (Array.isArray(result["text"])) {
            result["text"].push(_value);
          } else {
            result["text"] = [result["text"], _value];
          }
        }
        buffer = buffer.slice(_bytesToSkip);
        continue;
      }
      
      if (_objectId === 0x54) {
        // Raw sensor
        buffer = buffer.slice(1);
        _value = this.getRawValue(buffer);
        if (_value === null) break;
        _bytesToSkip = this.getVariableLengthSize(buffer);
        
        if (typeof result["raw"] === "undefined") {
          result["raw"] = _value;
        } else {
          if (Array.isArray(result["raw"])) {
            result["raw"].push(_value);
          } else {
            result["raw"] = [result["raw"], _value];
          }
        }
        buffer = buffer.slice(_bytesToSkip);
        continue;
      }
      
      // Handle fixed-length sensors
      if (typeof _bth === "undefined") {
        log("BTH: Unknown type", _objectId);
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
 * Sanitizes BTHome frame data to ensure JSON.stringify won't fail
 * Uses btoa() to encode binary strings as base64 for safe JSON transport
 * @param {Object} bthome - BTHome frame object
 * @returns {Object} Sanitized BTHome frame object
 */
function sanitizeBTHomeFrame(bthome) {
  if (!bthome || typeof bthome !== "object") return bthome;
  
  var sanitized = {};
  
  // Sanitize service_data - use btoa() to encode binary strings as base64
  if (bthome.service_data && typeof bthome.service_data === "object") {
    sanitized.service_data = {};
    for (var key in bthome.service_data) {
      if (bthome.service_data.hasOwnProperty(key)) {
        var value = bthome.service_data[key];
        sanitized.service_data[key] = typeof value === "string" ? btoa(value) : value;
      }
    }
  }
  
  // Sanitize manufacturer_data - use btoa() to encode binary strings as base64
  if (bthome.manufacturer_data && typeof bthome.manufacturer_data === "object") {
    sanitized.manufacturer_data = {};
    for (var key in bthome.manufacturer_data) {
      if (bthome.manufacturer_data.hasOwnProperty(key)) {
        var value = bthome.manufacturer_data[key];
        sanitized.manufacturer_data[key] = typeof value === "string" ? btoa(value) : value;
      }
    }
  }
  
  // local_name should be safe as it's typically ASCII
  if (bthome.local_name) {
    sanitized.local_name = bthome.local_name;
  }
  
  return sanitized;
}

/**
 * Еmitting the decoded BLE data to a specified event. It allows other scripts to receive and process the emitted data
 * @param {DeviceData} data
 */
function emitData(data) {
  if (typeof data !== "object") {
    return;
  }

  // Check if we should publish this event
  // Empty follows map = publish all; otherwise check if MAC is in follows
  var follows = getFollows();
  var followKeys = Object.keys(follows);
  var publishAll = followKeys.length === 0;
  var mac = normalizeMac(data.address);
  var follow = follows[mac];
  
  if (!publishAll && !follow) {
    return; // not followed and not in publish-all mode
  }

  if (MQTT.isConnected()) {
    topic = CONFIG.eventName + "/events/" + data.address;
    
    // Sanitize BTHome frame data before JSON.stringify to avoid UTF-8 errors
    var publishData = {};
    for (var key in data) {
      if (data.hasOwnProperty(key)) {
        if (key === "bthome") {
          publishData[key] = sanitizeBTHomeFrame(data[key]);
        } else {
          publishData[key] = data[key];
        }
      }
    }
    
    try {
      var payload = JSON.stringify(publishData);
      log("Publishing event via MQTT on topic: ", topic, "payload: ", payload);
      MQTT.publish(topic, payload, 1, false);
    } catch (e) {
      log("Error stringifying data for MQTT publish:", e);
      // Ensure 'e' is referenced so the minifier doesn't drop it and produce `catch {}`
      if (e && false) {}
    }
  }

  // Only emit local event if this MAC is specifically followed (not just publish-all)
  if (follow) {
    try {
      log("Emitting local event data: ", data);
      Shelly.emitEvent(CONFIG.eventName, data);
    } catch (e) {
      log("Error emitting local event: ", e);
      // Ensure 'e' is referenced so the minifier doesn't drop it and produce `catch {}`
      if (e && false) {}
    }
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

  // Add raw BTHome frame data for complete protocol information
  unpackedData.bthome = {};
  
  // Include service_data (contains BTHome UUID and data)
  if (result.service_data) {
    unpackedData.bthome.service_data = result.service_data;
  }
  
  // Include manufacturer_data if present
  if (result.manufacturer_data) {
    unpackedData.bthome.manufacturer_data = result.manufacturer_data;
  }
  
  // Include local_name if present
  if (result.local_name) {
    unpackedData.bthome.local_name = result.local_name;
  }

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

function onEventData(eventData) {
  try {
    if (eventData && eventData.info && eventData.info.event === "kvs") {
      var kvsEvent = eventData.info;
      // Check if the KVS change affects our prefix
      if (kvsEvent.key && kvsEvent.key.indexOf(CONFIG.kvsPrefix) === 0) {
        log("KVS change detected for key:", kvsEvent.key, "action:", kvsEvent.action);
        loadFollowsFromKVS();
      }
    } else if (eventData && eventData.info && eventData.info.event === "script_stop") {
      log("Script stopping");
    }
  } catch (e) {
    log("Error handling event:", e);
  }
}

function subscribeKvsEvents() {
  Shelly.addEventHandler(onEventData);
  log("Subscribed to KVS change events");
}

// Init, only if the Shelly object is available
if (typeof Shelly !== "undefined") {
  log("Script starting...");
  loadFollowsFromKVS();
  initBLEScanner();
  subscribeKvsEvents();
  log("Script initialization complete");
}
