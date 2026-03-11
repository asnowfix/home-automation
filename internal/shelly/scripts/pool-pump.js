// pool-pump.js
// ------------
//
// Unified script for pool pump control supporting two cooperating Shelly devices:
//
// Device 1 (3-output controller):
//   - Input 0: Low-water indication (turns OFF all outputs + enables water supply via wire)
//   - Input 1: High-water indication (reported as MQTT event)
//   - Output 0: Low speed
//   - Output 1: Medium speed  
//   - Output 2: High speed
//   - Rule: Only one output active at a time
//   - Button: Cycles through all-off → output 0 → output 1 → output 2 → repeat
//
// Device 2 (1-output override):
//   - Output 0: Full-speed override (when ON, disables Device 1 via wire)
//   - Button: Toggles on/off
//
// Features:
//   - Auto-detects device type at startup
//   - Persists state across reboots via KVS
//   - Low-water protection with state restoration
//   - MQTT events for water level sensors
//   - Physical button cycling

// === STATIC CONSTANTS ===
var SCRIPT_NAME = "pool-pump";
var CONFIG_KEY_PREFIX = "script/" + SCRIPT_NAME + "/";
var SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";

// Configuration schema
var CONFIG_SCHEMA = {
  enableLogging: {
    description: "Enable logging when true",
    key: "enable-logging",
    default: true,
    type: "boolean"
  },
  mqttTopicPrefix: {
    description: "MQTT topic prefix for water level events",
    key: "mqtt-topic-prefix",
    default: "pool/water",
    type: "string"
  }
};

// Runtime configuration values (initialized from defaults)
var CONFIG = {};

// Initialize CONFIG with default values
function initConfig() {
  for (var key in CONFIG_SCHEMA) {
    CONFIG[key] = CONFIG_SCHEMA[key].default;
  }
}

initConfig();

// State keys for KVS persistence
var STATE_KEYS = {
  activeOutput: "active-output"     // -1 (all off), 0, 1, 2 for 3-output; 0/1 for 1-output
};

// === STATE (DYNAMIC RUNTIME VALUES) ===
var STATE = {
  // Device configuration (auto-detected at startup)
  deviceType: null,           // "3-output" or "1-output"
  outputs: [],                // Array of available output IDs
  hasLowWater: false,         // Input 0 available
  hasHighWater: false,        // Input 1 available
  
  // Current state
  activeOutput: -1,           // Current active output (-1 = all off)
  savedOutput: -1,            // Saved output before low-water event
  lowWaterActive: false,      // Low-water protection active
  
  // MQTT connection
  mqttConnected: false,
  
  // Initialization flag
  initializing: true          // Prevents KVS writes during init
};

// === LOGGING ===
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
      if (e && false) {}
    }
    if (i + 1 < arguments.length) s += " ";
  }
  print(SCRIPT_PREFIX, s);
}

// === KVS HELPERS ===
function storeValue(key, value) {
  var valueStr;
  if (typeof value === "undefined" || value === null) {
    valueStr = "null";
  } else if (typeof value === "number" || typeof value === "boolean") {
    valueStr = value.toString();
  } else if (typeof value === "object") {
    valueStr = JSON.stringify(value);
  } else {
    valueStr = String(value);
  }
  // Fire-and-forget KVS.Set to avoid callback nesting
  Shelly.call("KVS.Set", {key: CONFIG_KEY_PREFIX + key, value: valueStr}, function(res, err) {
    if (err && false) {}  // Prevent minifier from removing error parameter
  });
}

function loadValue(key) {
  var result = Shelly.call("KVS.Get", {key: CONFIG_KEY_PREFIX + key});
  if (result && ("value" in result)) {
    var v = result.value;
    if (v === "null" || v === "undefined") return null;
    if (v === "true") return true;
    if (v === "false") return false;
    var num = Number(v);
    if (!isNaN(num)) return num;
    try {
      return JSON.parse(v);
    } catch (e) {
      if (e && false) {}
      return v;
    }
  }
  return null;
}

// === DEVICE DETECTION ===
function detectDeviceType() {
  log("Detecting device type...");
  
  // Check available outputs
  var availableOutputs = [];
  for (var i = 0; i < 10; i++) {
    var status = Shelly.getComponentStatus("switch:" + i);
    if (status && ("output" in status)) {
      availableOutputs.push(i);
    }
  }
  
  STATE.outputs = availableOutputs;
  
  // Check inputs
  var input0 = Shelly.getComponentStatus("input:0");
  var input1 = Shelly.getComponentStatus("input:1");
  STATE.hasLowWater = input0 && ("state" in input0);
  STATE.hasHighWater = input1 && ("state" in input1);
  
  // Determine device type
  if (availableOutputs.length >= 3) {
    STATE.deviceType = "3-output";
    log("Detected 3-output controller with outputs:", availableOutputs);
  } else if (availableOutputs.length === 1) {
    STATE.deviceType = "1-output";
    log("Detected 1-output override device");
  } else {
    log("WARNING: Unexpected output count:", availableOutputs.length);
    STATE.deviceType = availableOutputs.length > 1 ? "3-output" : "1-output";
  }
  
  log("Device type:", STATE.deviceType);
  log("Low-water input:", STATE.hasLowWater);
  log("High-water input:", STATE.hasHighWater);
}

// === STATE PERSISTENCE ===
function loadState() {
  log("Loading persisted state...");
  
  var savedActiveOutput = loadValue(STATE_KEYS.activeOutput);
  
  if (savedActiveOutput !== null) {
    STATE.activeOutput = savedActiveOutput;
    log("Restored active output:", STATE.activeOutput);
  }
}

function saveState() {
  // Skip KVS writes during initialization to avoid callback depth issues
  if (STATE.initializing) {
    return;
  }
  storeValue(STATE_KEYS.activeOutput, STATE.activeOutput);
}

// === OUTPUT CONTROL ===
function setOutput(outputId, on, callback) {
  if (STATE.outputs.indexOf(outputId) === -1) {
    log("ERROR: Invalid output ID:", outputId);
    if (callback) callback();
    return;
  }
  
  log("Setting output", outputId, "to", on);
  Shelly.call("Switch.Set", {id: outputId, on: on}, callback);
}

function activateOutput(outputId, callback) {
  log("Activating output:", outputId);
  
  if (STATE.deviceType === "3-output") {
    // Turn off all outputs first, then turn on the requested one
    var idx = 0;
    function processOutput() {
      if (idx >= STATE.outputs.length) {
        // All outputs off, now turn on the requested one if not -1
        if (outputId !== -1) {
          setOutput(outputId, true, function() {
            STATE.activeOutput = outputId;
            saveState();
            if (callback) callback();
          });
        } else {
          STATE.activeOutput = -1;
          saveState();
          if (callback) callback();
        }
        return;
      }
      
      setOutput(STATE.outputs[idx], false, function() {
        idx++;
        processOutput();
      });
    }
    processOutput();
  } else {
    // 1-output device: simple toggle
    var on = outputId === 0;
    setOutput(0, on, function() {
      STATE.activeOutput = on ? 0 : -1;
      saveState();
      if (callback) callback();
    });
  }
}

// === BUTTON HANDLING ===
function cycleOutputs() {
  log("Button pressed, cycling outputs");
  
  if (STATE.lowWaterActive) {
    log("Low-water protection active, ignoring button press");
    return;
  }
  
  if (STATE.deviceType === "3-output") {
    // Cycle: all off → 0 → 1 → 2 → all off
    var nextOutput;
    if (STATE.activeOutput === -1) {
      nextOutput = 0;
    } else if (STATE.activeOutput === 0) {
      nextOutput = 1;
    } else if (STATE.activeOutput === 1) {
      nextOutput = 2;
    } else {
      nextOutput = -1;
    }
    
    log("Cycling from", STATE.activeOutput, "to", nextOutput);
    activateOutput(nextOutput);
  } else {
    // 1-output device: toggle
    var nextOutput = STATE.activeOutput === -1 ? 0 : -1;
    log("Toggling from", STATE.activeOutput, "to", nextOutput);
    activateOutput(nextOutput);
  }
}

// === WATER LEVEL HANDLING ===
function publishWaterEvent(level, state) {
  if (!STATE.mqttConnected) return;
  
  var topic = CONFIG.mqttTopicPrefix + "/" + level;
  var message = JSON.stringify({
    level: level,
    state: state,
    timestamp: Date.now()
  });
  
  log("Publishing water event:", topic, message);
  MQTT.publish(topic, message, 0, false);
}

function handleLowWater(active) {
  log("Low-water event:", active);
  
  publishWaterEvent("low", active);
  
  if (active) {
    // Save current state and turn off all outputs
    STATE.savedOutput = STATE.activeOutput;
    STATE.lowWaterActive = true;
    log("Saving current output:", STATE.savedOutput);
    
    activateOutput(-1, function() {
      log("All outputs turned off for low-water protection");
    });
  } else {
    // Restore previous state
    STATE.lowWaterActive = false;
    log("Restoring output:", STATE.savedOutput);
    
    activateOutput(STATE.savedOutput, function() {
      log("Output restored after low-water cleared");
    });
  }
}

function handleHighWater(active) {
  log("High-water event:", active);
  publishWaterEvent("high", active);
}

// === EVENT HANDLERS ===
function handleSwitchEvent(info) {
  log("Switch event:", info);
  
  // Ignore switch events during low-water protection
  if (STATE.lowWaterActive && info.state === true) {
    log("Low-water protection active, turning off switch", info.id);
    setOutput(info.id, false);
    return;
  }
  
  if (STATE.deviceType === "3-output" && info.state === true) {
    // Ensure only one output is on
    var activatedOutput = info.id;
    for (var i = 0; i < STATE.outputs.length; i++) {
      var outputId = STATE.outputs[i];
      if (outputId !== activatedOutput) {
        setOutput(outputId, false);
      }
    }
    STATE.activeOutput = activatedOutput;
    saveState();
  } else if (STATE.deviceType === "1-output") {
    STATE.activeOutput = info.state ? 0 : -1;
    saveState();
  }
}

function handleInputEvent(info) {
  log("Input event:", info);
  
  if (info.component === "input:0") {
    handleLowWater(info.state);
  } else if (info.component === "input:1") {
    handleHighWater(info.state);
  }
}

function handleButtonEvent(info) {
  log("Button event:", info);
  
  // System button events: component="sys", event="brief_btn_down", name="sys"
  if (info.component === "sys" && info.event === "brief_btn_down") {
    cycleOutputs();
  }
}

// === MQTT SETUP ===
function setupMQTT() {
  var mqttStatus = Shelly.getComponentStatus("mqtt");
  if (mqttStatus && mqttStatus.connected) {
    STATE.mqttConnected = true;
    log("MQTT connected");
  } else {
    log("MQTT not connected");
  }
}

// === INITIALIZATION ===
function enforceOutputState() {
  log("Enforcing output state at startup...");
  
  if (STATE.deviceType === "3-output") {
    // Ensure only one output is on
    var onOutputs = [];
    for (var i = 0; i < STATE.outputs.length; i++) {
      var outputId = STATE.outputs[i];
      var status = Shelly.getComponentStatus("switch:" + outputId);
      if (status && status.output) {
        onOutputs.push(outputId);
      }
    }
    
    if (onOutputs.length > 1) {
      log("Multiple outputs on, keeping first:", onOutputs[0]);
      activateOutput(onOutputs[0]);
    } else if (onOutputs.length === 1) {
      STATE.activeOutput = onOutputs[0];
      saveState();
    } else {
      STATE.activeOutput = -1;
      saveState();
    }
  } else {
    // 1-output device
    var status = Shelly.getComponentStatus("switch:0");
    if (status) {
      STATE.activeOutput = status.output ? 0 : -1;
      saveState();
    }
  }
  
  log("Current active output:", STATE.activeOutput);
}

function init() {
  log("Script starting...");
  
  detectDeviceType();
  loadState();
  enforceOutputState();
  setupMQTT();
  
  // Check initial water level states
  if (STATE.hasLowWater) {
    var input0 = Shelly.getComponentStatus("input:0");
    if (input0 && input0.state) {
      handleLowWater(true);
    }
  }
  
  if (STATE.hasHighWater) {
    var input1 = Shelly.getComponentStatus("input:1");
    if (input1) {
      publishWaterEvent("high", input1.state);
    }
  }
  
  // Initialization complete - enable state persistence and save initial state
  STATE.initializing = false;
  
  // Defer state save to avoid callback nesting during init
  Timer.set(100, false, function() {
    storeValue(STATE_KEYS.activeOutput, STATE.activeOutput);
    log("Initial state saved to KVS");
  });
  
  log("Script initialization complete");
}

// === EVENT SUBSCRIPTION ===
Shelly.addEventHandler(function(event) {
  if (!event || !event.info) return;
  
  var info = event.info;
  
  // Handle script stop event
  if (info.event === "script_stop") {
    log("Script stopping");
    return;
  }
  
  // Handle component events
  if (typeof info.component === "string") {
    if (info.component.indexOf("switch:") === 0 && typeof info.state === "boolean") {
      handleSwitchEvent(info);
    } else if ((info.component === "input:0" || info.component === "input:1") && typeof info.state === "boolean") {
      handleInputEvent(info);
    } else if (info.component === "sys" && info.event === "brief_btn_down") {
      handleButtonEvent(info);
    }
  }
});

// Start the script
init();