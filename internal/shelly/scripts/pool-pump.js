// pool-pump.js
// ------------
//
// Master-slave pool pump control with time-based bootstrap and schedule automation:
//
// Device 1 (Pro3 - Master Controller):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pumps)
//   - Input 1: High-water sensor (MQTT event)
//   - Input 2: Max speed active from other device (MQTT event)
//   - Switch 0: Pump low/eco speed
//   - Switch 1: Pump mid speed
//   - Switch 2: Pump high speed
//   - Controls Pro1 via MQTT RPC for bootstrap sequence
//   - Tracks last run time for bootstrap decision
//   - Manages schedules for automated operation
//   - Button: Cycles through speeds
//
// Device 2 (Pro1 - Bootstrap Helper):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pump)
//   - Input 1: High-water sensor (MQTT event)
//   - Switch 0: Pump max speed for bootstrap
//   - Receives commands from Pro3 via MQTT RPC
//   - Button: Toggles on/off
//
// Features:
//   - Time-based bootstrap: Pro1 runs 2min at max speed when hours since last run > threshold
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
  bootstrapHoursThreshold: {
    description: "Hours since last run above which bootstrap is needed",
    key: "boot-hours",
    default: 6,
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
  },
  temperatureThreshold: {
    description: "Temperature threshold (°C) for summer mode (day schedule)",
    key: "temp-threshold",
    default: 20,
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

// Initialize CONFIG with defaults immediately so logging works
initConfig();

// Load configuration from KVS and validate required fields
function loadConfig(callback) {
  log("Loading configuration from KVS...");
  
  var missingRequired = [];
  var configKeys = [];
  
  // Build array of config keys to load
  for (var key in CONFIG_SCHEMA) {
    configKeys.push(key);
  }
  
  var keyIndex = 0;
  
  // Process one key at a time using task queue
  function loadNextKey() {
    if (keyIndex >= configKeys.length) {
      // All keys loaded, validate
      if (missingRequired.length > 0) {
        log("ERROR: Missing required configuration:");
        for (var i = 0; i < missingRequired.length; i++) {
          log("  -", missingRequired[i]);
        }
        log("Script cannot start without required configuration.");
        log("Please run: myhome ctl pool setup <controller-device>");
        callback(false);
        return;
      }
      
      // Role-specific validation
      if (CONFIG.deviceRole === "controller" && !CONFIG.bootstrapDeviceId) {
        log("ERROR: Controller role requires bootstrapDeviceId");
        callback(false);
        return;
      }
      
      log("Configuration loaded successfully");
      callback(true);
      return;
    }
    
    var key = configKeys[keyIndex];
    var schema = CONFIG_SCHEMA[key];
    var kvsKey = CONFIG_KEY_PREFIX + schema.key;
    keyIndex++;
    
    // Load from KVS asynchronously
    Shelly.call("KVS.Get", {key: kvsKey}, function(result, err) {
      if (err) {
        log("WARNING: KVS.Get failed for", kvsKey, ":", err, "- using default");
        CONFIG[key] = schema.default;
        if (schema.required && CONFIG[key] === null) {
          missingRequired.push(key + " (" + kvsKey + ") - KVS error: " + err);
        }
        queueTask(loadNextKey);
        return;
      }

      if (result && ("value" in result) && result.value !== null && result.value !== "") {
        var value = result.value;
        
        // Parse value based on type
        if (schema.type === "boolean") {
          CONFIG[key] = value === "true" || value === true;
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
      } else {
        // Use default
        CONFIG[key] = schema.default;
        
        // Check if required
        if (schema.required && CONFIG[key] === null) {
          missingRequired.push(key + " (" + kvsKey + ")");
        }
      }
      
      // Queue next key
      queueTask(loadNextKey);
    });
  }
  
  loadNextKey();
}

// Script.storage keys for continuously evolving values (survives reboots)
var STORAGE_KEYS = {
  forecastUrl: "forecast-url"       // Open-Meteo forecast URL built from device location
};

// State keys for KVS persistence
var STATE_KEYS = {
  activeOutput: "active-output",    // -1 (all off), 0, 1, 2 for pro3; 0/1 for pro1
  lastRunTimestamp: "last-run-ts",  // Unix timestamp (seconds) of last pump run
  scheduleMode: "schedule-mode"     // "summer" or "winter" mode
};

// === TASK QUEUE (SINGLE TIMER FOR ALL SEQUENTIAL OPERATIONS) ===
var TASK_QUEUE = [];
var TASK_INDEX = 0;
var TASK_TIMER = null;

function processTaskQueue() {
  if (TASK_INDEX >= TASK_QUEUE.length) {
    // No tasks left — stop timer and reset so queueTask() can restart it later
    if (TASK_TIMER) {
      Timer.clear(TASK_TIMER);
      TASK_TIMER = null;
    }
    TASK_QUEUE = [];
    TASK_INDEX = 0;
    return;
  }

  // Execute next task; new tasks queued by the task itself extend TASK_QUEUE
  // and will be picked up on subsequent timer ticks.
  var task = TASK_QUEUE[TASK_INDEX];
  TASK_INDEX++;
  task();
}

function queueTask(task) {
  // Simply append to queue
  TASK_QUEUE.push(task);
  
  // Start timer only if not already running
  if (!TASK_TIMER) {
    TASK_TIMER = Timer.set(200, true, processTaskQueue);
  }
}

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
  lastRunTimestamp: null,     // Unix timestamp (seconds) of last pump run
  
  // Bootstrap state
  bootstrapInProgress: false,
  bootstrapTimerId: null,
  nightRunTimerId: null,
  
  // MQTT connection
  mqttConnected: false,
  
  // Forecast cache (in-memory, refreshed daily)
  forecastUrl: null,          // Open-Meteo forecast URL
  cachedForecast: null,       // Array of hourly temperatures
  cachedForecastTimes: null,  // Array of hourly timestamps
  lastForecastFetchDate: null,// Date string (YYYY-M-D) of last fetch
  
  // Schedule mode
  scheduleMode: null,         // "summer" or "winter"
  
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

// === SCRIPT.STORAGE HELPERS (for forecast URL) ===
function storeStorageValue(key, value) {
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
  Script.storage.setItem(key, valueStr);
}

function loadStorageValue(key) {
  var v = Script.storage.getItem(key);
  if (v === null || v === undefined) return null;
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

// === BOOTSTRAP DECISION LOGIC ===
function needsBootstrap() {
  if (STATE.lastRunTimestamp === null) {
    log('No previous run recorded, bootstrap needed');
    return true;
  }
  
  var now = Math.floor(Date.now() / 1000); // Integer seconds (avoids float precision loss)
  var hoursSinceLastRun = (now - STATE.lastRunTimestamp) / 3600;
  var needs = hoursSinceLastRun > CONFIG.bootstrapHoursThreshold;
  
  log('Hours since last run:', hoursSinceLastRun.toFixed(1), 'threshold:', CONFIG.bootstrapHoursThreshold, 'hours, needs bootstrap:', needs);
  return needs;
}

// === WEATHER FORECAST FUNCTIONS ===
function setForecastURL(lat, lon) {
  log('setForecastURL', lat, lon);
  if (lat !== null && lon !== null) {
    var url = 'https://api.open-meteo.com/v1/forecast?latitude=' + lat + '&longitude=' + lon + '&hourly=temperature_2m&forecast_days=1&timezone=auto';
    STATE.forecastUrl = url;
    storeStorageValue(STORAGE_KEYS.forecastUrl, url);
    log('Forecast URL ready');
  }
}

function shouldRefreshForecast() {
  var now = new Date();
  var today = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
  
  if (STATE.lastForecastFetchDate === null || STATE.lastForecastFetchDate !== today) {
    return true;
  }
  return false;
}

function scheduleForecastRetry() {
  if (!STATE.cachedForecast) {
    log('Scheduling forecast retry in 10 seconds...');
    Timer.set(10000, false, function() {
      if (!STATE.cachedForecast) {
        log('Retrying forecast fetch...');
        fetchAndCacheForecast();
      }
    });
  }
}

function onForecast(result, error_code, error_message, cb) {
  if (error_code !== 0) {
    log('Forecast fetch error code:', error_code, 'message:', error_message);
    scheduleForecastRetry();
    if (typeof cb === 'function') cb();
    return;
  }
  
  if (!result || !result.body) {
    log('No forecast data in response');
    scheduleForecastRetry();
    if (typeof cb === 'function') cb();
    return;
  }
  
  var data = null;
  try {
    data = JSON.parse(result.body);
  } catch (e) {
    log('JSON parse error:', result.body);
    if (e && false) {}
    scheduleForecastRetry();
    if (typeof cb === 'function') cb();
    return;
  }
  
  if (!data || !data.hourly || !data.hourly.temperature_2m || data.hourly.temperature_2m.length === 0) {
    log('Invalid forecast structure');
    scheduleForecastRetry();
    if (typeof cb === 'function') cb();
    return;
  }
  
  STATE.cachedForecast = data.hourly.temperature_2m;
  STATE.cachedForecastTimes = data.hourly.time;
  data = null;
  
  var now = new Date();
  STATE.lastForecastFetchDate = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
  log('Forecast cached:', STATE.cachedForecast.length, 'values');
  
  if (typeof cb === 'function') {
    Timer.set(100, false, cb);
  }
}

function fetchAndCacheForecast(cb) {
  var url = STATE.forecastUrl || loadStorageValue(STORAGE_KEYS.forecastUrl);
  if (!url) {
    log('Open-Meteo forecast URL not configured. Skipping forecast.');
    if (typeof cb === 'function') cb();
    return;
  }
  
  log('Fetching fresh forecast from Open-Meteo...');
  Shelly.call("HTTP.GET", {
    url: url,
    timeout: 10
  }, onForecast, cb);
}

function getMaxForecastTemp() {
  if (!STATE.cachedForecast || STATE.cachedForecast.length === 0) {
    return null;
  }
  
  var maxTemp = null;
  for (var i = 0; i < STATE.cachedForecast.length; i++) {
    var temp = STATE.cachedForecast[i];
    if (temp !== null && (maxTemp === null || temp > maxTemp)) {
      maxTemp = temp;
    }
  }
  
  return maxTemp;
}

function onDeviceLocation(result, error_code, error_message, cb) {
  if (error_code === 0 && result) {
    if (result.lat !== null && result.lon !== null) {
      log('Auto-detected location: lat=' + result.lat + ', lon=' + result.lon);
      setForecastURL(result.lat, result.lon);
      if (typeof cb === 'function') cb();
    } else {
      log('Location detection returned null coordinates');
      if (typeof cb === 'function') cb();
    }
  } else {
    log('Location detection error:', error_code, error_message);
    if (typeof cb === 'function') cb();
  }
}

function ensureForecastUrl(cb) {
  if (STATE.forecastUrl) {
    if (typeof cb === 'function') cb();
    return;
  }
  
  STATE.forecastUrl = loadStorageValue(STORAGE_KEYS.forecastUrl);
  if (STATE.forecastUrl) {
    log('Loaded forecast URL from storage');
    if (typeof cb === 'function') cb();
    return;
  }
  
  log('Forecast URL not found, detecting location...');
  Shelly.call('Shelly.DetectLocation', {}, onDeviceLocation, cb);
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
    log('Long time since last run, executing bootstrap');
    executeBootstrap(targetSpeed, callback);
  } else {
    log('Recent run detected, direct Pro3 control');
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

function applyComponentNames(callback) {
  log("Applying component names to device...");
  
  var names = COMPONENT_NAMES[STATE.deviceType];
  if (!names) {
    if (callback) callback();
    return;
  }
  
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
  
  if (componentsToConfig.length === 0) {
    log("No components to configure");
    if (callback) callback();
    return;
  }
  
  // Process components sequentially using task queue
  var index = 0;
  
  function processNext() {
    if (index >= componentsToConfig.length) {
      log("All component names applied");
      if (callback) callback();
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
        queueTask(processNext);
      });
    } else if (comp.type === "switch") {
      Shelly.call("Switch.SetConfig", {id: comp.id, config: {name: comp.name}}, function(res, err) {
        if (err && false) {}
        log("Applied switch:" + comp.id + " name:", comp.name);
        queueTask(processNext);
      });
    }
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
  
  var savedLastRunTimestamp = loadValue(STATE_KEYS.lastRunTimestamp);
  if (savedLastRunTimestamp !== null) {
    STATE.lastRunTimestamp = savedLastRunTimestamp;
    log("Restored last run timestamp:", STATE.lastRunTimestamp);
  }
  
  var savedScheduleMode = loadValue(STATE_KEYS.scheduleMode);
  if (savedScheduleMode !== null) {
    STATE.scheduleMode = savedScheduleMode;
    log("Restored schedule mode:", STATE.scheduleMode);
  } else {
    STATE.scheduleMode = "winter";
    log("No saved schedule mode, defaulting to winter");
  }
}

function saveState() {
  // Skip KVS writes during initialization to avoid callback depth issues
  if (STATE.initializing) {
    return;
  }
  storeValue(STATE_KEYS.activeOutput, STATE.activeOutput);
  if (STATE.lastRunTimestamp !== null) {
    storeValue(STATE_KEYS.lastRunTimestamp, STATE.lastRunTimestamp);
  }
  if (STATE.scheduleMode !== null) {
    storeValue(STATE_KEYS.scheduleMode, STATE.scheduleMode);
  }
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
          // Record timestamp when pump starts
          STATE.lastRunTimestamp = Math.floor(Date.now() / 1000);
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
      // Record timestamp when pump starts (Pro1 bootstrap)
      if (on) {
        STATE.lastRunTimestamp = Math.floor(Date.now() / 1000);
      }
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
        queueTask(deleteNext);
      });
    }
    
    deleteNext();
  });
}

function createSchedules(callback) {
  log('Creating pool pump schedules...');
  
  var scriptId = Shelly.getCurrentScriptId();
  
  var schedules = [
    {
      enable: true,
      timespec: '@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleDailyCheck()'
        }
      }]
    },
    {
      enable: true,
      timespec: '@sunrise+3h * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleMorningStart()'
        }
      }]
    },
    {
      enable: true,
      timespec: '@sunset * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleEveningStop()'
        }
      }]
    },
    {
      enable: true,
      timespec: '0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleNightStart()'
        }
      }]
    },
    {
      enable: true,
      timespec: '0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleNightStop()'
        }
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
      queueTask(createNext);
    });
  }
  
  createNext();
}

// === SCHEDULE MODE MANAGEMENT ===
function updateScheduleMode(newMode) {
  if (STATE.scheduleMode === newMode) {
    log('Schedule mode already set to:', newMode);
    return;
  }
  
  log('Switching schedule mode from', STATE.scheduleMode, 'to', newMode);
  STATE.scheduleMode = newMode;
  saveState();
  
  // Enable/disable schedules based on mode
  Shelly.call('Schedule.List', {}, function(result, err) {
    if (err) {
      log('ERROR: Failed to list schedules:', err);
      if (err && false) {}
      return;
    }
    
    if (!result || !result.jobs) {
      log('No schedules found');
      return;
    }
    
    var schedulesToUpdate = [];
    for (var i = 0; i < result.jobs.length; i++) {
      var job = result.jobs[i];
      if (job.calls && job.calls.length > 0) {
        var code = job.calls[0].params && job.calls[0].params.code;
        if (code === 'handleMorningStart()' || code === 'handleEveningStop()') {
          schedulesToUpdate.push({id: job.id, enable: newMode === 'summer', name: code});
        } else if (code === 'handleNightStart()' || code === 'handleNightStop()') {
          schedulesToUpdate.push({id: job.id, enable: newMode === 'winter', name: code});
        }
      }
    }
    
    if (schedulesToUpdate.length === 0) {
      log('No schedules to update');
      return;
    }
    
    var updateIndex = 0;
    function updateNext() {
      if (updateIndex >= schedulesToUpdate.length) {
        log('All schedules updated for', newMode, 'mode');
        return;
      }
      
      var schedule = schedulesToUpdate[updateIndex];
      updateIndex++;
      
      Shelly.call('Schedule.Update', {id: schedule.id, enable: schedule.enable}, function(res, err) {
        if (err && false) {}
        log('Updated schedule', schedule.name, 'enable:', schedule.enable);
        queueTask(updateNext);
      });
    }
    
    updateNext();
  });
}

function performDailyModeCheck() {
  log('Performing daily mode check...');
  
  // Ensure forecast URL is configured
  ensureForecastUrl(function() {
    // Fetch or use cached forecast
    if (shouldRefreshForecast()) {
      log('Fetching fresh forecast for mode check...');
      fetchAndCacheForecast(function() {
        decideModeFromForecast();
      });
    } else {
      log('Using cached forecast for mode check');
      decideModeFromForecast();
    }
  });
}

function decideModeFromForecast() {
  var maxTemp = getMaxForecastTemp();
  
  if (maxTemp === null) {
    log('No forecast available, keeping current mode:', STATE.scheduleMode);
    return;
  }
  
  log('Max forecast temperature:', maxTemp, '°C, threshold:', CONFIG.temperatureThreshold, '°C');
  
  var newMode = maxTemp > CONFIG.temperatureThreshold ? 'summer' : 'winter';
  log('Determined mode:', newMode);
  
  updateScheduleMode(newMode);
}

// === SCHEDULE EVENT HANDLERS ===
function handleDailyCheck() {
  log('Daily check event');
  
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
  
  performDailyModeCheck();
}

function handleMorningStart() {
  log('Morning start event');
  
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
  
  if (!needsBootstrap()) {
    log('Recent run detected, starting eco speed');
    activateOutput(CONFIG.ecoSpeed);
  } else {
    log('Long time since last run, bootstrap required but not starting automatically');
  }
}


function handleEveningStop() {
  log('Evening stop event');
  
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
  
  // Clear any running timers
  if (STATE.nightRunTimerId) {
    Timer.clear(STATE.nightRunTimerId);
    STATE.nightRunTimerId = null;
  }
  
  activateOutput(-1, function() {
    log('Pump stopped for evening');
  });
}

function handleNightStart() {
  log('Night start event');
  
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
  
  if (needsBootstrap()) {
    log('Long time since last run, starting 1-hour high-speed run with bootstrap');
    
    executeBootstrap(CONFIG.highSpeed, function() {
      STATE.nightRunTimerId = Timer.set(CONFIG.nightRunDurationMs, false, function() {
        log('Night run duration complete, stopping pump');
        activateOutput(-1);
        STATE.nightRunTimerId = null;
      });
    });
  } else {
    log('Recent run detected, ignoring night-start event');
  }
}

function handleNightStop() {
  log('Night stop event');
  
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
  
  // Clear the in-script timer if it is still running (avoids a double stop)
  if (STATE.nightRunTimerId) {
    Timer.clear(STATE.nightRunTimerId);
    STATE.nightRunTimerId = null;
  }

  activateOutput(-1, function() {
    log('Pump stopped after night run');
  });
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
  
  // Load configuration from KVS first (asynchronous)
  loadConfig(function(success) {
    if (!success) {
      log("FATAL: Configuration validation failed, script cannot start");
      return;
    }
    
    continueInit();
  });
}

function continueInit() {
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
        log('Step 1/5: Detecting device type...');
        detectDeviceType();
        next();
      },
      function(next) {
        log('Step 2/5: Checking water supply status...');
        var input0 = Shelly.getComponentStatus('input:0');
        if (input0) {
          handleWaterSupply(input0.state);
        }
        next();
      },
      function(next) {
        log('Step 3/5: Configuring component names...');
        applyComponentNames(next);
      },
      function(next) {
        log('Step 4/5: Clearing old schedules...');
        clearNonUpdateSchedules(next);
      },
      function(next) {
        log('Step 5/5: Creating schedules...');
        createSchedules(next);
      }
    ];
    
    var stepIndex = 0;
    
    function runNextStep() {
      if (stepIndex >= initSteps.length) {
        log('✓ All initialization steps complete - script is now running');
        return;
      }
      
      var step = initSteps[stepIndex];
      stepIndex++;
      
      step(function() {
        queueTask(runNextStep);
      });
    }
    
    queueTask(runNextStep);
  } else {
    // Bootstrap helper - clean schedules but don't create pump schedules
    var initSteps = [
      function(next) {
        log('Step 1/2: Configuring component names...');
        applyComponentNames(next);
      },
      function(next) {
        log('Step 2/2: Clearing old schedules...');
        clearNonUpdateSchedules(next);
      }
    ];
    
    var stepIndex = 0;
    
    function runNextStep() {
      if (stepIndex >= initSteps.length) {
        log('✓ All initialization steps complete - script is now running');
        return;
      }
      
      var step = initSteps[stepIndex];
      stepIndex++;
      
      step(function() {
        queueTask(runNextStep);
      });
    }
    
    queueTask(runNextStep);
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