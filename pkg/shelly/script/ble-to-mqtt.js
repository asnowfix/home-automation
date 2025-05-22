// ble-to-mqtt.js - ES5 Compatible Version

print("Loading ble-to-mqtt.js");

let CONFIG = {
  debug: true,
  topic_prefix: "ble"
};

const MANUFACTURER_IGNORE = {
  "004c": "apple",
  "0ba9": "allterco",
  "0087": "garmin"
}

const MANUFACTURER = {
  "1501": "yifang",
  "0010": "inkbird"
}

// Simple logging function
function log(message) {
  if (CONFIG.debug) {
    print("[BLE-MQTT] " + message);
  }
}

function bin2hex (s) {
  var i, l, o = "", n;
  s += "";
  for (i = 0, l = s.length; i < l; i++) {
    n = s.charCodeAt(i).toString(16)
    o += n.length < 2 ? "0" + n : n;
  }
  return o;
}

// BLE scan callback function
function scanCB(ev, res) {
  if (ev !== BLE.Scanner.SCAN_RESULT) return;
  
  try {
    // Create a sanitized copy of the BLE data
    let data = {};
    
    // Copy only the safe properties we need
    if (res.addr) {
      data.addr = res.addr;
    }
    if (res.rssi) {
      data.rssi = res.rssi;
    }
    if (res.addr_type) {
      data.addr_type = res.addr_type;
    }
    if (res.advData) {
      data.advData = bin2hex(res.advData);
    }
    if (res.scanRsp) {
      data.scanRsp = bin2hex(res.scanRsp);
    }
    if (res.flags) {
      data.flags = res.flags;
    }
    if (res.service_uuids) {
      data.service_uuids = res.service_uuids;
    }
    if (res.manufacturer_data) {
      data.manufacturer = Object.keys(res.manufacturer_data)[0];
      data.manufacturer_data = bin2hex(res.manufacturer_data[data.manufacturer]);
    }

    // Stringify the sanitized data
    let msg = JSON.stringify(data);

    // Discard data from Apple and Allterco devices
    if (MANUFACTURER_IGNORE[data.manufacturer]) {
      return;
    }

    log(msg);

    // Check MQTT connection and publish
    if (MQTT.isConnected()) {
      MQTT.publish(CONFIG.topic_prefix + "/" + data.manufacturer + "/" + data.addr, msg, 1, false);
    }
  } catch (e) {
    print("Error in scanCB: " + e.toString());
  }
}

// Check if BLE is enabled
let bleConfig;
try {
  bleConfig = Shelly.getComponentConfig("ble");
  log("BLE config: " + JSON.stringify(bleConfig));
} catch (e) {
  print("Error getting BLE config: " + e);
}

// Exit if BLE isn't enabled
if (!bleConfig || !bleConfig.enable) {
  print("Error: Bluetooth is not enabled, please enable it in settings");
} else {
  // Define scan parameters
  const scanParams = {
    duration_ms: BLE.Scanner.INFINITE_SCAN,
    active: false
  };
  
  // Start the scanner if not already running
  if (!BLE.Scanner.isRunning()) {
    print("Starting BLE scanner");
    const scanner = BLE.Scanner.Start(scanParams);
    if (!scanner) {
      print("Error: Cannot start BLE scanner");
    } else {
      print("BLE scanner started successfully");
    }
  } else {
    print("BLE scanner already running");
  }
  
  // Subscribe to BLE events
  print("Subscribing to BLE scanner events");
  BLE.Scanner.Subscribe(scanCB);
  
  print("BLE to MQTT bridge initialized");
}