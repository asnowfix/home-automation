// pool-pump.js
// ------------
//
// Unified script for pool pump control supporting two cooperating Shelly devices:
//
// Device 1 (Pro3 - 3-switch controller):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pumps)
//   - Input 1: High-water sensor (MQTT event)
//   - Input 2: Max speed active from other device (MQTT event)
//   - Switch 0: Pump low/eco speed
//   - Switch 1: Pump mid speed
//   - Switch 2: Pump high speed
//   - Rule: Only one switch active at a time
//   - Button: Cycles through speeds
//
// Device 2 (Pro1 - 1-switch override):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pump)
//   - Input 1: High-water sensor (MQTT event)
//   - Switch 0: Pump max speed (disables Pro3 switches via wire to Pro3 input:2)
//   - Button: Toggles on/off
//
// Features:
//   - Auto-detects device type at startup
//   - Configures component names and input inversion at startup
//   - Persists state across reboots via KVS
//   - Water supply protection with state restoration
//   - MQTT events for all input and switch transitions
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
    description: "MQTT topic prefix for events",
    key: "mqtt-topic-prefix",
    default: "pool/pump",
    type: "string"
  }
};

// Component names by device type
var COMPONENT_NAMES = {
  "pro3": {
    inputs: [
      {id: 0, name: "water-supply", invert: true},
      {id: 1, name: "high-water", invert: false},
      {id: 2, name: "max-speed-active", invert: false}
    ],
    switches: [
      {id: 0, name: "pump-eco"},
      {id: 1, name: "pump-mid"},
      {id: 2, name: "pump-high"}
    ]
  },
  "pro1": {
    inputs: [
      {id: 0, name: "water-supply", invert: true},
      {id: 1, name: "high-water", invert: false}
    ],
    switches: [
      {id: 0, name: "pump-max"}
    ]
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
  activeOutput: "active-output"     // -1 (all off), 0, 1, 2 for pro3; 0/1 for pro1
};

// === STATE (DYNAMIC RUNTIME VALUES) ===
var STATE = {
  // Device configuration (auto-detected at startup)
  deviceType: null,           // "pro3" or "pro1"
  outputs: [],                // Array of available output IDs
  inputs: [],                 // Array of available input IDs
  
  // Component name mappings
  inputNames: {},             // {id: name}
  switchNames: {},            // {id: name}
  
  // Current state
  activeOutput: -1,           // Current active output (-1 = all off)
  savedOutput: -1,            // Saved output before water-supply protection
  
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
  
  // Check available switches
  var availableOutputs = [];
  for (var i = 0; i < 4; i++) {
    var status = Shelly.getComponentStatus("switch:" + i);
    if (status && ("output" in status)) {
      availableOutputs.push(i);
    }
  }
  STATE.outputs = availableOutputs;
  
  // Check available inputs
  var availableInputs = [];
  for (var i = 0; i < 4; i++) {
    var status = Shelly.getComponentStatus("input:" + i);
    if (status && ("state" in status)) {
      availableInputs.push(i);
    }
  }
  STATE.inputs = availableInputs;
  
  // Determine device type
  if (availableOutputs.length >= 3) {
    STATE.deviceType = "pro3";
    log("Detected Pro3 (3-switch controller)");
  } else if (availableOutputs.length === 1) {
    STATE.deviceType = "pro1";
    log("Detected Pro1 (1-switch override)");
  } else {
    log("WARNING: Unexpected switch count:", availableOutputs.length);
    STATE.deviceType = availableOutputs.length > 1 ? "pro3" : "pro1";
  }
  
  log("Device type:", STATE.deviceType);
  log("Switches:", availableOutputs);
  log("Inputs:", availableInputs);
}

// === COMPONENT NAMING ===
function configureComponentNames() {
  log("Configuring component names...");
  
  var names = COMPONENT_NAMES[STATE.deviceType];
  if (!names) {
    log("ERROR: No component names defined for device type:", STATE.deviceType);
    return;
  }
  
  // Build name mappings (synchronous)
  for (var i = 0; i < names.inputs.length; i++) {
    var input = names.inputs[i];
    if (STATE.inputs.indexOf(input.id) !== -1) {
      STATE.inputNames[input.id] = input.name;
    }
  }
  
  for (var i = 0; i < names.switches.length; i++) {
    var sw = names.switches[i];
    if (STATE.outputs.indexOf(sw.id) !== -1) {
      STATE.switchNames[sw.id] = sw.name;
    }
  }
}

function applyComponentNames() {
  log("Applying component names to device...");
  
  var names = COMPONENT_NAMES[STATE.deviceType];
  if (!names) return;
  
  // Build list of components to configure
  var componentsToConfig = [];
  
  for (var i = 0; i < names.inputs.length; i++) {
    var input = names.inputs[i];
    if (STATE.inputs.indexOf(input.id) !== -1) {
      componentsToConfig.push({
        type: "input", 
        id: input.id, 
        name: input.name,
        invert: input.invert
      });
    }
  }
  
  for (var i = 0; i < names.switches.length; i++) {
    var sw = names.switches[i];
    if (STATE.outputs.indexOf(sw.id) !== -1) {
      componentsToConfig.push({type: "switch", id: sw.id, name: sw.name});
    }
  }
  
  // Process components sequentially with a single recurring timer
  var index = 0;
  
  function processNext() {
    if (index >= componentsToConfig.length) {
      log("All component names applied");
      return;
    }
    
    var comp = componentsToConfig[index];
    index++;
    
    if (comp.type === "input") {
      var config = {name: comp.name};
      if (typeof comp.invert === "boolean") {
        config.invert = comp.invert;
      }
      Shelly.call("Input.SetConfig", {id: comp.id, config: config}, function(res, err) {
        if (err && false) {}
        log("Applied input:" + comp.id + " config:", JSON.stringify(config));
      });
    } else if (comp.type === "switch") {
      Shelly.call("Switch.SetConfig", {id: comp.id, config: {name: comp.name}}, function(res, err) {
        if (err && false) {}
        log("Applied switch:" + comp.id + " name:", comp.name);
      });
    }
    
    // Schedule next component
    Timer.set(100, false, processNext);
  }
  
  processNext();
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
  
  log("Setting switch", outputId, "to", on);
  Shelly.call("Switch.Set", {id: outputId, on: on}, callback);
}

function activateOutput(outputId, callback) {
  log("Activating output:", outputId);
  
  if (STATE.deviceType === "pro3") {
    // Turn off all outputs simultaneously
    for (var i = 0; i < STATE.outputs.length; i++) {
      Shelly.call("Switch.Set", {id: STATE.outputs[i], on: false}, function(res, err) {
        if (err && false) {}  // Prevent minifier from removing error parameter
      });
    }
    
    // Wait for outputs to turn off, then activate the requested one
    Timer.set(200, false, function() {
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
    });
  } else {
    // Pro1: simple toggle
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
  
  // Check if water supply is active
  var input0 = Shelly.getComponentStatus("input:0");
  if (input0 && input0.state) {
    log("Water supply protection active, ignoring button press");
    return;
  }
  
  if (STATE.deviceType === "pro3") {
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
    // Pro1: toggle
    var nextOutput = STATE.activeOutput === -1 ? 0 : -1;
    log("Toggling from", STATE.activeOutput, "to", nextOutput);
    activateOutput(nextOutput);
  }
}

// === WATER SUPPLY PROTECTION ===
function handleWaterSupply(waterSupplyActive) {
  log("Water supply active signal:", waterSupplyActive);
  
  if (waterSupplyActive) {
    // Water supply is ON (signal is HIGH after invert) - save current state and turn off all pumps
    STATE.savedOutput = STATE.activeOutput;
    log("Water supply ON - saving current output:", STATE.savedOutput);
    
    activateOutput(-1, function() {
      log("All pumps turned off for water supply protection");
    });
  } else {
    // Water supply is OFF (signal is LOW after invert) - restore previous state
    log("Water supply OFF - restoring output:", STATE.savedOutput);
    
    activateOutput(STATE.savedOutput, function() {
      log("Pump restored after water supply turned off");
    });
  }
}

// === EVENT HANDLERS ===
function handleSwitchEvent(info) {
  log("Switch event:", info);
  
  // Ignore switch events during water supply protection
  var input0 = Shelly.getComponentStatus("input:0");
  if (input0 && input0.state && info.state === true) {
    log("Water supply protection active, turning off switch", info.id);
    setOutput(info.id, false);
    return;
  }
  
  if (STATE.deviceType === "pro3" && info.state === true) {
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
  } else if (STATE.deviceType === "pro1") {
    STATE.activeOutput = info.state ? 0 : -1;
    saveState();
  }
}

function handleInputEvent(info) {
  log("Input event:", info);
  
  // Handle input:0 (water-supply)
  if (info.id === 0) {
    handleWaterSupply(info.state);
  }
  // Input:1 (high-water) and input:2 (max-speed-active) are just notifications
}

function handleButtonEvent(info) {
  log("Button event:", info);
  
  // System button events (component="sys"):
  // - sys_btn_down: Button pressed down
  // - sys_btn_up: Button released
  // - sys_btn_push: Complete brief push (down + up)
  // - brief_btn_down: Legacy event (deprecated, use sys_btn_push instead)
  
  if (info.component === "sys" && info.event === "sys_btn_push") {
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
  
  if (STATE.deviceType === "pro3") {
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
    // Pro1
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
  configureComponentNames();
  loadState();
  enforceOutputState();
  setupMQTT();
  
  // Initialization complete - enable state persistence
  STATE.initializing = false;
  
  log("Script initialization complete");
  
  // Sequential initialization steps with single timer
  var initSteps = [
    // Step 1: Save initial state
    function() {
      storeValue(STATE_KEYS.activeOutput, STATE.activeOutput);
      log("Initial state saved to KVS");
    },
    // Step 2: Check water supply
    function() {
      var input0 = Shelly.getComponentStatus("input:0");
      if (input0) {
        handleWaterSupply(input0.state);
      }
    },
    // Step 3: Apply component names
    function() {
      applyComponentNames();
    }
  ];
  
  var stepIndex = 0;
  
  function runNextStep() {
    if (stepIndex >= initSteps.length) {
      log("All initialization steps complete");
      return;
    }
    
    initSteps[stepIndex]();
    stepIndex++;
    
    Timer.set(200, false, runNextStep);
  }
  
  // Start sequential initialization after 100ms
  Timer.set(100, false, runNextStep);
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
    } else if (info.component.indexOf("input:") === 0 && typeof info.state === "boolean") {
      handleInputEvent(info);
    } else if (info.component === "sys" && info.event === "sys_btn_push") {
      handleButtonEvent(info);
    } else {
      log("Unhandled component event:", JSON.stringify(info));
    }
  } else {
    log("Unhandled event:", JSON.stringify(info));
  }
});

// Start the script
init();