// pool-pump.js
// ------------
//
// Master-slave pool pump control with temperature-based bootstrap and schedule automation:
//
// Device 1 (Pro3 - Master Controller):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pumps)
//   - Input 1: High-water sensor (MQTT event)
//   - Input 2: Max speed active from other device (MQTT event)
//   - Switch 0: Pump low/eco speed
//   - Switch 1: Pump mid speed
//   - Switch 2: Pump high speed
//   - Controls Pro1 via MQTT RPC for bootstrap sequence
//   - Monitors outdoor temperature for bootstrap decision
//   - Manages schedules for automated operation
//   - Button: Cycles through speeds
//
// Device 2 (Pro1 - Bootstrap Helper):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pump)
//   - Input 1: High-water sensor (MQTT event)
//   - Switch 0: Pump max speed for cold-weather bootstrap
//   - Receives commands from Pro3 via MQTT RPC
//   - Button: Toggles on/off
//
// Features:
//   - Temperature-based bootstrap: Pro1 runs 2min at max speed when temp < threshold
//   - Schedule-driven automation: morning-start (SR+3h), evening-stop (SS), night-start (23:15)
//   - MQTT RPC communication between Pro3 and Pro1
//   - Auto-detects device role based on device ID
//   - Water supply protection with state restoration
//   - Physical button cycling

// === STATIC CONSTANTS ===
var SCRIPT_NAME = "pool-pump";
var CONFIG_KEY_PREFIX = "script/" + SCRIPT_NAME + "/";
var SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";

// Configuration schema
var CONFIG_SCHEMA = {
  enableLogging: {
    description: "Enable logging when true",
    key: "logging",
    default: true,
    type: "boolean"
  },
  mqttTopicPrefix: {
    description: "MQTT topic prefix for events",
    key: "mqtt-topic",
    default: "pool/pump",
    type: "string"
  },
  deviceRole: {
    description: "Device role: 'controller' or 'bootstrap' (required)",
    key: "device-role",
    default: null,
    type: "string",
    required: true
  },
  controllerDeviceId: {
    description: "Controller device ID (required)",
    key: "controller-id",
    default: null,
    type: "string",
    required: true
  },
  bootstrapDeviceId: {
    description: "Bootstrap device ID (required for controller role)",
    key: "bootstrap-id",
    default: null,
    type: "string",
    required: false
  },
  temperatureTopic: {
    description: "MQTT topic for outdoor temperature sensor (optional)",
    key: "temp-topic",
    default: null,
    type: "string",
    required: false
  },
  bootstrapThreshold: {
    description: "Temperature threshold (°C) below which bootstrap is needed",
    key: "boot-threshold",
    default: 15,
    type: "number",
    required: false
  },
  ecoSpeed: {
    description: "Pro3 switch ID for eco/low speed (0, 1, or 2)",
    key: "eco-speed",
    default: 0,
    type: "number",
    required: false
  },
  midSpeed: {
    description: "Pro3 switch ID for mid speed (0, 1, or 2)",
    key: "mid-speed",
    default: 1,
    type: "number",
    required: false
  },
  highSpeed: {
    description: "Pro3 switch ID for high speed (0, 1, or 2)",
    key: "high-speed",
    default: 2,
    type: "number",
    required: false
  },
  bootstrapDurationMs: {
    description: "Bootstrap duration in milliseconds",
    key: "boot-duration",
    default: 120000,
    type: "number",
    required: false
  },
  nightRunDurationMs: {
    description: "Night run duration in milliseconds",
    key: "night-duration",
    default: 3600000,
    type: "number",
    required: false
  },
  bootstrapToSpeedDelayMs: {
    description: "Delay between bootstrap end and real pump speed start in milliseconds",
    key: "boot-delay",
    default: 500,
    type: "number",
    required: false
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

// Load configuration from KVS and validate required fields
function loadConfig() {
  log("Loading configuration from KVS...");
  
  var missingRequired = [];
  
  for (var key in CONFIG_SCHEMA) {
    var schema = CONFIG_SCHEMA[key];
    var kvsKey = CONFIG_KEY_PREFIX + schema.key;
    
    // Try to load from KVS
    var result = Shelly.call("KVS.Get", {key: kvsKey});
    
    if (result && ("value" in result) && result.value !== null && result.value !== "") {
      var value = result.value;
      
      // Parse value based on type
      if (schema.type === "boolean") {
        CONFIG[key] = value === "true";
      } else if (schema.type === "number") {
        var num = Number(value);
        if (!isNaN(num)) {
          CONFIG[key] = num;
        } else {
          log("WARNING: Invalid number for", key, ":", value);
          CONFIG[key] = schema.default;
        }
      } else {
        CONFIG[key] = value;
      }
      
      log("Loaded", key, "=", CONFIG[key]);
    } else {
      // Use default
      CONFIG[key] = schema.default;
      
      // Check if required
      if (schema.required && CONFIG[key] === null) {
        missingRequired.push(key + " (" + kvsKey + ")");
      }
    }
  }
  
  // Validate required fields
  if (missingRequired.length > 0) {
    log("ERROR: Missing required configuration:");
    for (var i = 0; i < missingRequired.length; i++) {
      log("  -", missingRequired[i]);
    }
    log("Script cannot start without required configuration.");
    log("Please run: myhome ctl pool setup <controller-device>");
    return false;
  }
  
  // Role-specific validation
  if (CONFIG.deviceRole === "controller" && !CONFIG.bootstrapDeviceId) {
    log("ERROR: Controller role requires bootstrapDeviceId");
    return false;
  }
  
  log("Configuration loaded successfully");
  return true;
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
  deviceRole: null,           // "pro3-controller" or "pro1-helper"
  deviceId: null,             // Actual device ID from system
  outputs: [],                // Array of available output IDs
  inputs: [],                 // Array of available input IDs
  
  // Component name mappings
  inputNames: {},             // {id: name}
  switchNames: {},            // {id: name}
  
  // Current state
  activeOutput: -1,           // Current active output (-1 = all off)
  savedOutput: -1,            // Saved output before water-supply protection
  
  // Temperature monitoring
  temperature: {
    outdoor: null             // Outdoor temperature in °C
  },
  subscribedTemperatureTopic: null,
  
  // Bootstrap state
  bootstrapInProgress: false,
  bootstrapTimerId: null,
  nightRunTimerId: null,
  
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

// === TEMPERATURE MONITORING ===
/**
 * Parse temperature from MQTT message (supports Gen1, Gen2, BLU formats)
 * @param {string} topic - MQTT topic
 * @param {string} message - MQTT message payload
 * @returns {number|null} Parsed temperature or null if not found
 */
function parseTemperatureFromMqtt(topic, message) {
  var temp = null;
  try {
    // H&T Gen1 format, via gen1 HTTP-to-MQTT proxy
    // topic: shellies/<id>/sensor/temperature
    // message: plain number payload
    if (topic.indexOf('shellies/') === 0 && topic.indexOf('/sensor/temperature') > 0) {
      temp = parseFloat(message);
      if (!isNaN(temp)) {
        log('Parsed Gen1 temperature:', temp, 'from topic:', topic);
        return temp;
      }
    }
    // H&T BLU Gen3 format, via blu-publisher.js script
    // topic: shelly-blu/events/<mac>
    // message: {"temperature":17,...}
    if (topic.indexOf('shelly-blu/events/') === 0) {
      var data = JSON.parse(message);
      if (data.temperature) {
        log('Parsed BLU Gen3 temperature:', data.temperature, 'from topic:', topic);
        return data.temperature;
      }
    }
    // Gen2 format: <id>/events/rpc with JSON payload
    // topic: <id>/events/rpc
    // message: {"method":"NotifyStatus","params":{"temperature:0":{"tC":22.5}}}
    else if (topic.indexOf('/events/rpc') > 0) {
      var data = JSON.parse(message);
      if (data.method === 'NotifyStatus' && data.params) {
        if (data.params['temperature:0'] && typeof data.params['temperature:0'].tC !== 'undefined') {
          temp = data.params['temperature:0'].tC;
        } else if (data.params['temperature:1'] && typeof data.params['temperature:1'].tC !== 'undefined') {
          temp = data.params['temperature:1'].tC;
        } else if (data.params['temperature:2'] && typeof data.params['temperature:2'].tC !== 'undefined') {
          temp = data.params['temperature:2'].tC;
        }
        if (temp !== null) {
          log('Parsed Gen2 temperature:', temp, 'from topic:', topic);
          return temp;
        }
      }
    }
  } catch (e) {
    log('Error parsing temperature from MQTT:', e);
    if (e && false) {}  // Prevent minifier from removing error parameter
  }
  return null;
}

function onOutdoorTemperature(topic, message) {
  var temp = parseTemperatureFromMqtt(topic, message);
  if (temp !== null) {
    STATE.temperature.outdoor = temp;
    log('Outdoor temperature updated:', temp, '°C');
  }
}

function subscribeToTemperature() {
  if (CONFIG.temperatureTopic && !STATE.subscribedTemperatureTopic) {
    log('Subscribing to temperature topic:', CONFIG.temperatureTopic);
    MQTT.subscribe(CONFIG.temperatureTopic, onOutdoorTemperature);
    STATE.subscribedTemperatureTopic = CONFIG.temperatureTopic;
  }
}

function needsBootstrap() {
  if (STATE.temperature.outdoor === null) {
    log('No temperature data, assuming bootstrap needed');
    return true;
  }
  var needs = STATE.temperature.outdoor < CONFIG.bootstrapThreshold;
  log('Temperature:', STATE.temperature.outdoor, '°C, threshold:', CONFIG.bootstrapThreshold, '°C, needs bootstrap:', needs);
  return needs;
}

// === MQTT RPC CONTROL (Pro3 -> Pro1) ===
function sendPro1Command(method, params, callback) {
  if (STATE.deviceRole !== 'controller') {
    log('ERROR: Only controller can send commands to bootstrap device');
    if (callback) callback('Not controller');
    return;
  }
  
  if (!CONFIG.bootstrapDeviceId) {
    log('ERROR: Bootstrap device ID not configured');
    if (callback) callback('Bootstrap device ID not configured');
    return;
  }
  
  var requestId = Math.floor(Math.random() * 1000000);
  var topic = CONFIG.bootstrapDeviceId + '/rpc';
  var request = {
    id: requestId,
    src: CONFIG.controllerDeviceId,
    method: method,
    params: params || {}
  };
  
  log('Sending RPC to Pro1:', method, 'params:', JSON.stringify(params));
  
  MQTT.publish(topic, JSON.stringify(request), 0, false, function(success) {
    if (success) {
      log('RPC request sent successfully');
      if (callback) callback(null);
    } else {
      log('ERROR: Failed to send RPC request');
      if (callback) callback('MQTT publish failed');
    }
  });
}

function turnOnPro1(callback) {
  sendPro1Command('Switch.Set', {id: 0, on: true}, callback);
}

function turnOffPro1(callback) {
  sendPro1Command('Switch.Set', {id: 0, on: false}, callback);
}

// === BOOTSTRAP SEQUENCE ===
function executeBootstrap(targetSpeed, callback) {
  if (STATE.deviceRole !== 'controller') {
    log('ERROR: Only controller can execute bootstrap');
    if (callback) callback();
    return;
  }
  
  if (STATE.bootstrapInProgress) {
    log('Bootstrap already in progress, ignoring');
    if (callback) callback();
    return;
  }
  
  log('Starting bootstrap sequence...');
  STATE.bootstrapInProgress = true;
  
  // Phase 1: Turn on Pro1 for bootstrap
  turnOnPro1(function(err) {
    if (err) {
      log('ERROR: Failed to turn on Pro1:', err);
      STATE.bootstrapInProgress = false;
      STATE.bootstrapTimerId = null;
      if (callback) callback();
      return;
    }
    
    log('Pro1 activated, waiting', CONFIG.bootstrapDurationMs, 'ms...');
    
    // Phase 2: After bootstrap duration, turn off Pro1 and activate Pro3
    STATE.bootstrapTimerId = Timer.set(CONFIG.bootstrapDurationMs, false, function() {
      log('Bootstrap duration complete, switching to controller control');
      STATE.bootstrapTimerId = null;
      
      // Turn off Pro1
      turnOffPro1(function(err) {
        if (err) {
          log('ERROR: Failed to turn off Pro1:', err);
          STATE.bootstrapInProgress = false;
          if (callback) callback();
          return;
        }
        
        // Wait configured delay before activating target speed on controller
        log('Waiting', CONFIG.bootstrapToSpeedDelayMs, 'ms before starting controller at target speed');
        Timer.set(CONFIG.bootstrapToSpeedDelayMs, false, function() {
          activateOutput(targetSpeed, function() {
            log('Bootstrap complete, controller now controlling at speed:', targetSpeed);
            STATE.bootstrapInProgress = false;
            if (callback) callback();
          });
        });
      });
    });
  });
}

function startPumpWithBootstrap(targetSpeed, callback) {
  if (needsBootstrap()) {
    log('Temperature below threshold, executing bootstrap');
    executeBootstrap(targetSpeed, callback);
  } else {
    log('Temperature above threshold, direct Pro3 control');
    activateOutput(targetSpeed, callback);
  }
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

// === SCHEDULE MANAGEMENT ===
function clearNonUpdateSchedules(callback) {
  log('Clearing existing schedules...');
  
  Shelly.call('Schedule.List', {}, function(result, err) {
    if (err) {
      log('ERROR: Failed to list schedules:', err);
      if (err && false) {}
      if (callback) callback();
      return;
    }
    
    if (!result || !result.jobs) {
      log('No schedules found');
      if (callback) callback();
      return;
    }
    
    var jobsToDelete = [];
    for (var i = 0; i < result.jobs.length; i++) {
      var job = result.jobs[i];
      // Keep only device auto-update schedules
      if (job.calls && job.calls.length > 0) {
        var isUpdate = false;
        for (var j = 0; j < job.calls.length; j++) {
          if (job.calls[j].method === 'Shelly.Update') {
            isUpdate = true;
            break;
          }
        }
        if (!isUpdate) {
          jobsToDelete.push(job.id);
        }
      }
    }
    
    if (jobsToDelete.length === 0) {
      log('No schedules to delete');
      if (callback) callback();
      return;
    }
    
    log('Deleting', jobsToDelete.length, 'schedules');
    var deleteIndex = 0;
    
    function deleteNext() {
      if (deleteIndex >= jobsToDelete.length) {
        log('All schedules deleted');
        if (callback) callback();
        return;
      }
      
      var jobId = jobsToDelete[deleteIndex];
      deleteIndex++;
      
      Shelly.call('Schedule.Delete', {id: jobId}, function(res, err) {
        if (err && false) {}
        log('Deleted schedule:', jobId);
        Timer.set(100, false, deleteNext);
      });
    }
    
    deleteNext();
  });
}

function createSchedules(callback) {
  log('Creating pool pump schedules...');
  
  var schedules = [
    {
      enable: true,
      timespec: '0 0 SR+3h * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'Shelly.EmitEvent',
        params: {event: 'pool-pump/morning-start'}
      }]
    },
    {
      enable: true,
      timespec: '0 0 SS * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'Shelly.EmitEvent',
        params: {event: 'pool-pump/evening-stop'}
      }]
    },
    {
      enable: true,
      timespec: '0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'Shelly.EmitEvent',
        params: {event: 'pool-pump/night-start'}
      }]
    }
  ];
  
  var scheduleIndex = 0;
  
  function createNext() {
    if (scheduleIndex >= schedules.length) {
      log('All schedules created');
      if (callback) callback();
      return;
    }
    
    var schedule = schedules[scheduleIndex];
    scheduleIndex++;
    
    Shelly.call('Schedule.Create', schedule, function(res, err) {
      if (err) {
        log('ERROR: Failed to create schedule:', err);
        if (err && false) {}
      } else {
        log('Created schedule:', schedule.timespec);
      }
      Timer.set(200, false, createNext);
    });
  }
  
  createNext();
}

// === DEVICE EVENT HANDLERS ===
function handleDeviceEvent(eventName) {
  log('Received device event:', eventName);
  
  if (STATE.deviceRole !== 'controller') {
    log('Ignoring event, not controller');
    return;
  }
  
  // Check water supply protection
  var input0 = Shelly.getComponentStatus('input:0');
  if (input0 && input0.state) {
    log('Water supply protection active, ignoring event');
    return;
  }
  
  if (eventName === 'pool-pump/morning-start') {
    handleMorningStart();
  } else if (eventName === 'pool-pump/evening-stop') {
    handleEveningStop();
  } else if (eventName === 'pool-pump/night-start') {
    handleNightStart();
  }
}

function handleMorningStart() {
  log('Morning start event');
  
  if (!needsBootstrap()) {
    log('Temperature above threshold, starting eco speed');
    activateOutput(CONFIG.ecoSpeed);
  } else {
    log('Temperature below threshold, bootstrap required but not starting automatically');
    // Don't auto-start in morning if cold - user decision
  }
}

function handleEveningStop() {
  log('Evening stop event');
  
  // Clear any running timers
  if (STATE.nightRunTimerId) {
    Timer.clear(STATE.nightRunTimerId);
    STATE.nightRunTimerId = null;
  }
  
  // Stop pump regardless of current state
  activateOutput(-1, function() {
    log('Pump stopped for evening');
  });
}

function handleNightStart() {
  log('Night start event');
  
  if (needsBootstrap()) {
    log('Temperature below threshold, starting 1-hour high-speed run');
    
    // Start with bootstrap, then run high speed for 1 hour
    executeBootstrap(CONFIG.highSpeed, function() {
      // Set timer to stop after night run duration
      STATE.nightRunTimerId = Timer.set(CONFIG.nightRunDurationMs, false, function() {
        log('Night run duration complete, stopping pump');
        activateOutput(-1);
        STATE.nightRunTimerId = null;
      });
    });
  } else {
    log('Temperature above threshold, ignoring night-start event');
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
  
  // Load configuration from KVS first
  if (!loadConfig()) {
    log("FATAL: Configuration validation failed, script cannot start");
    return;
  }
  
  detectDeviceType();
  
  // Get device ID from system
  var deviceInfo = Shelly.getComponentStatus('sys');
  if (deviceInfo && ("device_id" in deviceInfo)) {
    STATE.deviceId = deviceInfo.device_id;
    log('Device ID:', STATE.deviceId);
  }
  
  // Use role from configuration
  STATE.deviceRole = CONFIG.deviceRole;
  log('Device role:', STATE.deviceRole);
  
  configureComponentNames();
  loadState();
  enforceOutputState();
  setupMQTT();
  
  // Initialization complete - enable state persistence
  STATE.initializing = false;
  
  log("Script initialization complete");
  
  // Sequential initialization for controller
  if (STATE.deviceRole === 'controller') {
    var initSteps = [
      function(next) {
        storeValue(STATE_KEYS.activeOutput, STATE.activeOutput);
        log('Initial state saved to KVS');
        next();
      },
      function(next) {
        var input0 = Shelly.getComponentStatus('input:0');
        if (input0) {
          handleWaterSupply(input0.state);
        }
        next();
      },
      function(next) {
        applyComponentNames();
        next();
      },
      function(next) {
        subscribeToTemperature();
        next();
      },
      function(next) {
        clearNonUpdateSchedules(next);
      },
      function(next) {
        createSchedules(next);
      }
    ];
    
    var stepIndex = 0;
    
    function runNextStep() {
      if (stepIndex >= initSteps.length) {
        log('All initialization steps complete');
        return;
      }
      
      var step = initSteps[stepIndex];
      stepIndex++;
      
      step(function() {
        Timer.set(200, false, runNextStep);
      });
    }
    
    Timer.set(100, false, runNextStep);
  } else {
    // Bootstrap helper - clean schedules but don't create pump schedules
    var initSteps = [
      function(next) {
        applyComponentNames();
        next();
      },
      function(next) {
        clearNonUpdateSchedules(next);
      }
    ];
    
    var stepIndex = 0;
    
    function runNextStep() {
      if (stepIndex >= initSteps.length) {
        log('All initialization steps complete');
        return;
      }
      
      var step = initSteps[stepIndex];
      stepIndex++;
      
      step(function() {
        Timer.set(200, false, runNextStep);
      });
    }
    
    Timer.set(100, false, runNextStep);
  }
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
  
  // Handle device events (pool-pump/morning-start, evening-stop, night-start)
  if (typeof info.event === "string" && info.event.indexOf("pool-pump/") === 0) {
    handleDeviceEvent(info.event);
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