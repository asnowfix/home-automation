// Kalman filter heater controller for Shelly Plus 1 (ES5 style, no Node.js, for Shelly scripting engine)
// Uses Open-Meteo API for weather forecasts (no API key required)

// === STATIC CONSTANTS ===
const SCRIPT_NAME = "heater";
const CONFIG_KEY_PREFIX = 'script/' + SCRIPT_NAME + '/';
const SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";
const DEFAULT_COOLING_RATE = 1.0;

var CONFIG = {
  // Configuration values (loaded from KVS or defaults)
  enableLogging: true,
  setpoint: 19.0,
  minInternalTemp: 15.0,
  cheapStartHour: 23,
  cheapEndHour: 7,
  pollIntervalMs: 5 * 60 * 1000,
  preheatHours: 2,
  normallyClosed: true,
  internalTemperatureTopic: null,
  externalTemperatureTopic: null,
};

var CONFIG_KEY = {
  // Configuration keys (to load from KVS)
  enableLogging: "enable-logging",
  setpoint: "set-point",
  minInternalTemp: "min-internal-temp",
  cheapStartHour: "cheap-start-hour",
  cheapEndHour: "cheap-end-hour",
  pollIntervalMs: "poll-interval-ms",
  preheatHours: "preheat-hours",
  normallyClosed: "normally-closed",
  internalTemperatureTopic: "internal-temperature-topic",
  externalTemperatureTopic: "external-temperature-topic",
};

// Script.storage keys for continuously evolving values
var STORAGE_KEYS = {
  coolingRate: "cooling-rate",
  forecastUrl: "forecast-url",
  lastCheapEnd: "last-cheap-end",
  internalTemp: "internal-temp",
  externalTemp: "external-temp"
};

function getCoolingRate() {
  var v = Script.storage.getItem(STORAGE_KEYS.coolingRate);
  return (typeof v === "number") ? v : DEFAULT_COOLING_RATE;
}

function setCoolingRate(v) {
  if (typeof v === "number") {
    Script.storage.setItem(STORAGE_KEYS.coolingRate, v);
  }
}

/**
 * Script.storage key: "cooling-rate"
 * Stores the continuously learned cooling coefficient as a number.
 */

/**
 * Script.storage key: "forecast-url"
 * Stores the Open-Meteo forecast URL string built from detected device location.
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

// === STATE (DYNAMIC RUNTIME VALUES) ===
var STATE = {
  // Occupancy URL (built from MQTT server IP + /status)
  occupancyUrl: null,
  
  // Forecast cache
  forecastUrl: null,
  cachedForecast: null,
  cachedForecastTimes: null,
  lastForecastFetchDate: null,
  
  // Heater state
  lastHeaterState: false,
  normallyClosed: true,
  
  // Readiness tracking
  forecastUrlReady: false,
  forecastDataReady: false,
  internalTempReady: false,
  externalTempReady: false,
};

function detectLocationAndLoadConfig() {
  // Use stored forecast URL if available
  var stored = getForecastUrl();
  if (stored) {
    STATE.forecastUrl = stored;
    STATE.forecastUrlReady = true;
    log('Using stored forecast URL');
    checkAndStartControlLoop();
    loadConfig();
    return;
  }

  log('Detecting device location...');
  Shelly.call('Shelly.DetectLocation', {}, function(result, error_code, error_message) {
    if (error_code === 0 && result) {
      if (result.lat !== null && result.lon !== null) {
        log('Auto-detected location: lat=' + result.lat + ', lon=' + result.lon + ', tz=' + result.tz);
        updateForecastURL(result.lat, result.lon);
      } else {
        log('Location detection returned null coordinates');
      }
    } else {
      log('Failed to detect location (error ' + error_code + '): ' + error_message);
    }
    // Proceed to load config after attempting to set forecast URL
    loadConfig();
  });
}

function getForecastUrl() {
  var u = Script.storage.getItem(STORAGE_KEYS.forecastUrl);
  return (typeof u === 'string' && u.length > 0) ? u : null;
}

function updateForecastURL(lat, lon) {
  if (lat !== null && lon !== null) {
    var url = 'https://api.open-meteo.com/v1/forecast?latitude=' + lat + '&longitude=' + lon + '&hourly=temperature_2m&forecast_days=1&timezone=auto';
    STATE.forecastUrl = url;
    STATE.forecastUrlReady = true;
    Script.storage.setItem(STORAGE_KEYS.forecastUrl, url);
    log('Forecast URL ready');
    checkAndStartControlLoop();
  }
}

function loadConfig() {
  Shelly.call('KVS.GetMany', {}, function(result, error_code, error_message) {
    if (error_code === 0 && result && result.items) {
      log('KVS config loaded, processing', result.items.length, 'items');
      try {
        // Loop through all KVS items
        for (var i = 0; i < result.items.length; i++) {
          var item = result.items[i];
          var itemKey = item.key;
          
          // Check if this key matches any of our config keys
          for (var configName in CONFIG_KEY) {
            var fullKey = CONFIG_KEY_PREFIX + CONFIG_KEY[configName];
            
            if (itemKey === fullKey) {
              var valueStr = item.value;
              var value;
              
              // Try JSON parse first (handles objects, arrays, booleans, numbers, strings)
              try {
                value = JSON.parse(valueStr);
              } catch (e) {
                // Not valid JSON, try parsing as primitive types
                if (valueStr === "true" || valueStr === "false") {
                  // Boolean string
                  value = valueStr === "true";
                } else {
                  // Try as number (parseFloat handles both integers and floats)
                  var numValue = parseFloat(valueStr);
                  if (!isNaN(numValue)) {
                    value = numValue;
                  } else {
                    // Keep as string
                    value = valueStr;
                  }
                }
              }
              
              CONFIG[configName] = value;
              log('Loaded config', configName, '=', value, 'from key', itemKey);
              break;
            }
          }
          
          // Also check for normally-closed in switch component KVS
          if (itemKey === 'normally-closed') {
            CONFIG.normallyClosed = item.value === 'true';
            log('Loaded normally-closed =', CONFIG.normallyClosed);
          }
        }
      } catch (e) {
        log('Error loading KVS config:', e);
      }
    } else {
      log('Failed to load KVS config (error ' + error_code + '): ' + error_message);
    }
  });
}

function saveConfigValue(key, value) {
  var valueStr = JSON.stringify(value);
  Shelly.call('KVS.Set', { key: CONFIG_KEY_PREFIX + key, value: valueStr }, function(result, error_code, error_msg) {
    if (error_code) {
      log('Error saving config', key, ':', error_msg);
    } else {
      log('Saved config', key, ':', valueStr);
    }
  });
}

// Call this once at script start - detect location first, then load config
detectLocationAndLoadConfig();

// === TIME WINDOW FOR HEATING ===
function isCheapHour() {
  var now = new Date();
  var hour = now.getHours();
  return (hour >= CONFIG.cheapStartHour || hour < CONFIG.cheapEndHour);
}

function getFilteredTemp() {
  return kalman.lastMeasurement ? kalman.lastMeasurement() : null;
}

function onCheapWindowEnd() {
  var temp = getFilteredTemp();
  if (temp !== null) {
    var now = (new Date()).getTime();
    var data = { temp: temp, time: now };
    Script.storage.setItem(STORAGE_KEYS.lastCheapEnd, JSON.stringify(data));
    log("Stored end-of-cheap-window temp:", temp);
  }
}

function onCheapWindowStart() {
  var storedData = Script.storage.getItem(STORAGE_KEYS.lastCheapEnd);
  if (!storedData) {
    log("No previous cheap window end data available for learning");
    return;
  }
  
  var data = null;
  try {
    data = JSON.parse(storedData);
  } catch (e) {
    log("Failed to parse last cheap end data");
    return;
  }
  
  if (!data || !data.temp || !data.time) {
    log("Invalid last cheap end data");
    return;
  }
  
  var prevTemp = data.temp;
  var prevTime = data.time;
  var now = (new Date()).getTime();
  var hours = (now - prevTime) / (3600 * 1000);
  var currTemp = getFilteredTemp();
  if (currTemp !== null && hours > 0) {
    var rate = (prevTemp - currTemp) / hours;
    // Update moving average
    var oldRate = getCoolingRate();
    var newRate = 0.7 * oldRate + 0.3 * rate; // EMA
    setCoolingRate(newRate);
    log("Updated cooling rate:", newRate, "from", oldRate);
  }
}

// Schedule learning events at CHEAP_START_HOUR and CHEAP_END_HOUR
function scheduleLearningTimers() {
  var now = new Date();
  var hour = now.getHours();
  var scheduleAt = function(targetHour, cb) {
    var delay = (targetHour - hour) * 3600000;
    if (delay < 0) delay += 24 * 3600000;
    Timer.set(delay, false, function() {
      cb();
      // Re-schedule for next day
      Timer.set(24 * 3600000, true, cb);
    });
  };
  scheduleAt(CONFIG.cheapEndHour, onCheapWindowEnd);
  scheduleAt(CONFIG.cheapStartHour, onCheapWindowStart);
}

// Call this once at script start
scheduleLearningTimers();

function initOccupancyUrl(cb) {
  log('initOccupancyUrl');
  // Try to get MQTT status synchronously
  var cfg = Shelly.getComponentConfig('mqtt');
  if (cfg && typeof cfg === 'object') {
    if ("server" in cfg && typeof cfg.server === 'string') {
      // server = "192.168.1.2:1883"
      var host = cfg.server;
      var i = host.indexOf(':');
      if (i >= 0) host = host.substring(0, i);
      STATE.occupancyUrl = 'http://' + host + ':8889/status';
      log('Occupancy URL set to', STATE.occupancyUrl);
      if (cb) cb(STATE.occupancyUrl);
    }
  }
}

// === PRE-HEATING LOGIC ===
function minutesUntilCheapEnd() {
  var now = new Date();
  var hour = now.getHours();
  var minute = now.getMinutes();
  var end = CONFIG.cheapEndHour;
  var minutesNow = hour * 60 + minute;
  var minutesEnd = end * 60;
  if (end <= CONFIG.cheapStartHour) minutesEnd += 24 * 60; // handle overnight windows
  if (minutesNow > minutesEnd) minutesEnd += 24 * 60; // handle wrap-around
  return minutesEnd - minutesNow;
}

// === ADVANCED COOLING MODEL: LOSS DEPENDS ON TEMP DIFFERENCE ===
// We now use: predictedDrop = COOLING_COEFF * (filteredTemp - externalTemp) * hoursToEnd
// COOLING_COEFF is learned as before (from data)

function shouldPreheat(filteredTemp, forecastTemp, mfTemp, cb) {
  k = getCoolingRate(); // k is now a cooling coefficient (per hour)
  var minutesToEnd = minutesUntilCheapEnd();
  var hoursToEnd = minutesToEnd / 60.0;
  // Use the lowest forecast for the next N hours for external temp
  var futureExternal = null;
  if (forecastTemp !== null && mfTemp !== null) futureExternal = Math.min(forecastTemp, mfTemp);
  else if (forecastTemp !== null) futureExternal = forecastTemp;
  else if (mfTemp !== null) futureExternal = mfTemp;
  // Fallback to current external temp if no forecast
  if (futureExternal === null && typeof lastExternalTemp !== 'undefined') {
    futureExternal = lastExternalTemp;
  }
  // If still null, fallback to 0
  if (futureExternal === null) futureExternal = 0;
  // Predict indoor temp at end of cheap window using exponential model
  // T_end = T_start - k * (T_start - T_ext) * hours
  var predictedDrop = k * (filteredTemp - futureExternal) * hoursToEnd;
  var predictedTemp = filteredTemp - predictedDrop;
  cb((hoursToEnd <= CONFIG.preheatHours) && (predictedTemp < CONFIG.setpoint));
}

// Store last measured external temp for fallback in shouldPreheat
var lastExternalTemp = null;

// === PARALLEL DATA FETCH HELPERS (reduce callback nesting) ===
// Note: Must be defined BEFORE being patched below (no hoisting in Shelly JS)
function fetchAllControlInputs(cb) {
  // Check if we need to refresh the forecast (once per day)
  if (shouldRefreshForecast()) {
    fetchAndCacheForecast(function(success) {
      if (success) {
        log('Forecast cache refreshed successfully');
      } else {
        log('Forecast cache refresh failed, will use stale data if available');
      }
      // Continue with control inputs regardless of fetch success
      fetchControlInputsWithCachedForecast(cb);
    });
  } else {
    // Use cached forecast
    fetchControlInputsWithCachedForecast(cb);
  }
}

function fetchControlInputsWithCachedForecast(cb) {
  var results = { internal: null, external: null, forecast: null, occupied: null };
  var done = 0;
  var total = 3; // Only 3 now: internal, external, occupancy (forecast is from cache)
  
  function check() {
    done++;
    if (done === total) {
      // Get forecast from cache
      results.forecast = getCurrentForecastTemp();
      if (results.forecast !== null) {
        log('Using cached forecast: ' + results.forecast + '°C');
      }
      cb(results);
    }
  }
  
  getShellyTemperature('internal', function(val) { results.internal = val; check(); });
  getShellyTemperature('external', function(val) { results.external = val; check(); });
  getOccupancy(function(val) { results.occupied = val; check(); });
}

// Patch fetchAllControlInputs to store last external temp
var origFetchAll = fetchAllControlInputs;

fetchAllControlInputs = function(cb) {
  origFetchAll(function(results) {
    if (results.external !== null) lastExternalTemp = results.external;
    log('Fetched all control inputs:', results);
    cb(results);
  });
};

function getOccupancy(cb) {
  if (!STATE.occupancyUrl) {
    log('Occupancy URL not configured, assuming not occupied');
    cb(false);
    return;
  }
  
  Shelly.call("HTTP.GET", {
    url: STATE.occupancyUrl,
    timeout: 5
  }, function(result, error_code, error_message) {
    if (error_code === 0 && result && result.body) {
      var data = null;
      try { data = JSON.parse(result.body); } catch (e) {}
      cb(data && data.occupied === true);
    } else {
      log('Error fetching occupancy status:', error_message);
      cb(false); // Default: not occupied
    }
  });
}

// === KALMAN FILTER IMPLEMENTATION (ES5) ===
function KalmanFilter(R, Q, A, B, C) {
  this.R = typeof R !== 'undefined' ? R : 0.01;
  this.Q = typeof Q !== 'undefined' ? Q : 1;
  this.A = typeof A !== 'undefined' ? A : 1;
  this.B = typeof B !== 'undefined' ? B : 0;
  this.C = typeof C !== 'undefined' ? C : 1;
  this.cov = NaN;
  this.x = NaN;
}
KalmanFilter.prototype.filter = function(z, u) {
  if (typeof u === 'undefined') u = 0;
  if (isNaN(this.x)) {
    this.x = (1 / this.C) * z;
    this.cov = (1 / this.C) * this.Q * (1 / this.C);
  } else {
    var predX = this.A * this.x + this.B * u;
    var predCov = this.A * this.cov * this.A + this.R;
    var K = predCov * this.C / (this.C * predCov * this.C + this.Q);
    this.x = predX + K * (z - this.C * predX);
    this.cov = predCov - K * this.C * predCov;
  }
  return this.x;
};
KalmanFilter.prototype.lastMeasurement = function() {
  return this.x;
};

// Initialize Kalman filter instance
var kalman = new KalmanFilter();

// === MQTT TEMPERATURE HANDLING ===
// Detect if topic is Gen1 or Gen2 format and extract temperature
function parseTemperatureFromMqtt(topic, message) {
  var temp = null;
  log("parseTemperatureFromMqtt", topic, message);
  try {
    // Gen1 format: shellies/<id>/sensor/temperature with plain number payload
    if (topic.indexOf('shellies/') === 0 && topic.indexOf('/sensor/temperature') > 0) {
      temp = parseFloat(message);
      if (!isNaN(temp)) {
        log('Parsed Gen1 temperature:', temp, 'from topic:', topic);
        return temp;
      }
    }
    // Gen2 format: <id>/events/rpc with JSON payload
    else if (topic.indexOf('/events/rpc') > 0) {
      var data = JSON.parse(message);
      // Look for temperature in NotifyStatus params
      if (data.method === 'NotifyStatus' && data.params) {
        // Check various temperature component formats
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
    else {
      log('Unknown topic format:', topic);
    }
  } catch (e) {
    log('Error parsing temperature from MQTT:', e);
  }
  return null;
}

function onTemperature(key, topic, message, userdata) {
  var temp = parseTemperatureFromMqtt(topic, message);
  if (temp !== null) {
    // Store in Script.storage (synchronous, internal data)
    Script.storage.setItem(key, temp);
    log('Stored ' + key + ' temperature:', temp);
    
    // Check if we now have all required temperatures
    checkTemperaturesReady();
  }
}

// Extract device name from MQTT topic (e.g., "shellies/device-name/sensor/temperature" -> "device-name")
function extractDeviceNameFromTopic(topic) {
  if (!topic) return null;
  var parts = topic.split('/');
  if (parts.length >= 2) {
    return parts[1];
  }
  return null;
}


// Generate a simple random request ID
function generateRequestId() {
  var chars = 'abcdefghijklmnopqrstuvwxyz0123456789';
  var id = '';
  for (var i = 0; i < 16; i++) {
    id += chars[Math.floor(Math.random() * chars.length)];
  }
  return id;
}

// Check if we have all required temperatures
function checkTemperaturesReady() {
  var internalTemp = Script.storage.getItem(STORAGE_KEYS.internalTemp);
  var externalTemp = Script.storage.getItem(STORAGE_KEYS.externalTemp);
  
  var hasInternal = (internalTemp !== null && internalTemp !== undefined);
  var hasExternal = (externalTemp !== null && externalTemp !== undefined);
  
  if (hasInternal && !STATE.internalTempReady) {
    STATE.internalTempReady = true;
    log('Internal temperature ready:', internalTemp);
    checkAndStartControlLoop();
  }
  
  if (hasExternal && !STATE.externalTempReady) {
    STATE.externalTempReady = true;
    log('External temperature ready:', externalTemp);
    checkAndStartControlLoop();
  }
}

/**
 * Request `myhome` to republish its last cached value for the given topic.
 * @param {Request} topic 
 */
function requestMqttRepeat(topic) {
  var request = JSON.stringify({
    id: generateRequestId(),
    src: clientId,
    replyTo: responseTopic,
    dst: 'myhome',
    method: 'mqtt.repeat',
    params: topic
  });

  log('Publishing request to myhome/rpc:', request);
  
  MQTT.publish('myhome/rpc', request, 0, false);
}

// Fetch initial temperatures at startup if missing
function fetchInitialTemperatures() {
  log('Checking for missing temperatures at startup...');
  
  // Check internal temperature
  var internalTemp = Script.storage.getItem(STORAGE_KEYS.internalTemp);
  if ((internalTemp === null || internalTemp === undefined) && CONFIG.internalTemperatureTopic) {
    requestMqttRepeat(CONFIG.internalTemperatureTopic)
    // Give a chance of the temperature to be republished on the topic
    Timer.setTimer(1000, checkTemperaturesReady)
  }
  
  // Check external temperature
  var externalTemp = Script.storage.getItem(STORAGE_KEYS.externalTemp);
  if ((externalTemp === null || externalTemp === undefined) && CONFIG.externalTemperatureTopic) {
    requestMqttRepeat(CONFIG.externalTemperatureTopic)
    // Give a chance of the temperature to be republished on the topic
    Timer.setTimer(1000, checkTemperaturesReady)
  }
}

// Subscribe to MQTT topics for temperature sources
function subscribeMqttTemperatures() {
  log('Subscribing to MQTT topics for temperature sources...');
  if (CONFIG.internalTemperatureTopic) {
    log('Subscribing to internal temperature topic:', CONFIG.internalTemperatureTopic);
    MQTT.subscribe(CONFIG.internalTemperatureTopic, onTemperature.bind(null, STORAGE_KEYS.internalTemp), null);
  }
  if (CONFIG.externalTemperatureTopic) {
    log('Subscribing to external temperature topic:', CONFIG.externalTemperatureTopic);
    MQTT.subscribe(CONFIG.externalTemperatureTopic, onTemperature.bind(null, STORAGE_KEYS.externalTemp), null);
  }
  
  // Fetch initial temperatures if missing
  log('About to call fetchInitialTemperatures...');
  try {
    fetchInitialTemperatures();
  } catch (e) {
    log('Error calling fetchInitialTemperatures:', e);
  }
}

// === DATA FETCHING FUNCTIONS ===
// Read temperature from Script.storage (stored by MQTT callbacks)
function getShellyTemperature(location, cb) {
  var key = location === 'internal' ? STORAGE_KEYS.internalTemp : STORAGE_KEYS.externalTemp;
  var temp = Script.storage.getItem(key);
  
  if (temp !== null && temp !== undefined) {
    log('Read', location, 'temperature:', temp);
    cb(temp);
  } else {
    log('No', location, 'temperature available yet');
    cb(null);
  }
}

function shouldRefreshForecast() {
  var now = new Date();
  var today = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
  
  // Refresh if never fetched or if it's a new day
  if (STATE.lastForecastFetchDate === null || STATE.lastForecastFetchDate !== today) {
    return true;
  }
  return false;
}

function fetchAndCacheForecast(cb) {
  var url = STATE.forecastUrl || getForecastUrl();
  if (!url) {
    log('Open-Meteo forecast URL not configured. Skipping forecast.');
    cb(null);
    return;
  }
  
  log('Fetching fresh forecast from Open-Meteo...');
  Shelly.call("HTTP.GET", {
    url: url,
    timeout: 10
  }, function(result, error_code, error_message) {
    if (error_code === 0 && result && result.body) {
      var data = null;
      try { data = JSON.parse(result.body); } catch (e) {}
      
      if (data && data.hourly && data.hourly.temperature_2m && data.hourly.temperature_2m.length > 0) {
        // Cache the full forecast arrays
        STATE.cachedForecast = data.hourly.temperature_2m;
        STATE.cachedForecastTimes = data.hourly.time;
        var now = new Date();
        STATE.lastForecastFetchDate = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
        STATE.forecastDataReady = true;
        log('Cached forecast with ' + STATE.cachedForecast.length + ' hourly values for date: ' + STATE.lastForecastFetchDate);
        checkAndStartControlLoop();
        cb(true);
      } else {
        log('Failed to parse forecast data');
        cb(false);
      }
    } else {
      log('Error fetching Open-Meteo forecast:', error_message);
      cb(false);
    }
  });
}

function getCurrentForecastTemp() {
  if (!STATE.cachedForecast || STATE.cachedForecast.length === 0) {
    return null;
  }
  
  var now = new Date();
  var currentHour = now.getHours();
  
  // Use current hour as index (Open-Meteo returns data starting from 00:00 today)
  var idx = currentHour < STATE.cachedForecast.length ? currentHour : 0;
  var temp = STATE.cachedForecast[idx];
  
  return temp;
}

// Get minimum forecast temperature for the next N hours
function getMinForecastTemp(hours) {
  if (!STATE.cachedForecast || STATE.cachedForecast.length === 0) {
    return null;
  }
  
  var now = new Date();
  var currentHour = now.getHours();
  
  // Calculate how many hours to look ahead (capped by available forecast data)
  var hoursToCheck = Math.min(Math.ceil(hours), STATE.cachedForecast.length - currentHour);
  if (hoursToCheck <= 0) {
    return getCurrentForecastTemp();
  }
  
  // Find minimum temperature in the next N hours
  var minTemp = null;
  for (var i = 0; i < hoursToCheck; i++) {
    var idx = currentHour + i;
    if (idx < STATE.cachedForecast.length) {
      var temp = STATE.cachedForecast[idx];
      if (temp !== null && temp !== undefined) {
        if (minTemp === null || temp < minTemp) {
          minTemp = temp;
        }
      }
    }
  }
  
  return minTemp;
}

function controlHeaterWithInputs(results) {
  var internalTemp = results.internal;
  var externalTemp = results.external;
  var forecastTemp = results.forecast;
  var isOccupied = results.occupied;
  log('Internal:', internalTemp, 'External:', externalTemp, 'Forecast:', forecastTemp, 'Occupied:', isOccupied);
  if (internalTemp === null) {
    log('Skipping control cycle due to missing internal temperature');
    return;
  }
  var controlInput = 0;
  var count = 0;
  if (externalTemp !== null) { controlInput += externalTemp; count++; }
  if (forecastTemp !== null) { controlInput += forecastTemp; count++; }
  if (count > 0) controlInput = controlInput / count;
  var filteredTemp = kalman.filter(internalTemp, controlInput);
  log('Filtered temperature:', filteredTemp);
  var heaterShouldBeOn = filteredTemp < CONFIG.setpoint;
  // SAFETY: If filtered temperature is below minInternalTemp, always heat IF occupied
  if (isOccupied && filteredTemp < CONFIG.minInternalTemp) {
    log('Safety override: internal temp', filteredTemp, 'below minInternalTemp', CONFIG.minInternalTemp, '=> HEAT');
    setHeaterState(true);
    return;
  }
  // Calculate minimum forecast temperature for preheat window
  var mfTemp = getMinForecastTemp(CONFIG.preheatHours);
  log('Minimum forecast temp for next', CONFIG.preheatHours, 'hours:', mfTemp);
  shouldPreheat(filteredTemp, forecastTemp, mfTemp, function(preheat) {
    if ((heaterShouldBeOn && isCheapHour()) || preheat) {
      log('Heater ON (normal or preheat mode)', 'preheat:', preheat);
      setHeaterState(true);
    } else {
      log('Outside cheap window => no heating');
      setHeaterState(false);
    }
  });
}

// === MAIN CONTROL LOOP (flattened) ===
var controlLoopStarted = false;

function pollAndControl() {
  // Only run control if we have all necessary inputs
  if (!STATE.forecastUrlReady || !STATE.forecastDataReady || !STATE.internalTempReady || !STATE.externalTempReady) {
    log('Skipping control cycle - waiting for initialization (url:', STATE.forecastUrlReady, 'forecast:', STATE.forecastDataReady, 'internal:', STATE.internalTempReady, 'external:', STATE.externalTempReady, ')');
    return;
  }
  
  fetchAllControlInputs(controlHeaterWithInputs);
}

// Check if ready and start control loop
function checkAndStartControlLoop() {
  log('Checking wether we can start control loop')
  log('  - Forecast URL ready:' + STATE.forecastUrlReady);
  log('  - Forecast data ready:' + STATE.forecastDataReady);
  log('  - Internal temp ready:' + STATE.internalTempReady);
  log('  - External temp ready:' + STATE.externalTempReady);
  if (STATE.forecastUrlReady && STATE.forecastDataReady && STATE.internalTempReady && STATE.externalTempReady) {
    if (!controlLoopStarted) {
      controlLoopStarted = true;
      log('All inputs ready - starting control loop');
      // Run initial control cycle
      pollAndControl();
    }
  }
}

// === HEATER CONTROL (LOCAL SHELLY CALL, SUPPORTS normally-closed VALUE) ===
function setHeaterState(on) {
  STATE.lastHeaterState = on;
  var newState = on !== CONFIG.normallyClosed
  Shelly.call("Switch.Set", { id: 0, on: newState }, function(result, error_code, error_msg) {
    if (error_code) {
      log('Error setting heater switch state:', error_msg);
    } else {
      log('Heater switch set to', on, "(result:", result, ")");
    }
  });
}

// === SCHEDULED EXECUTION ===
log("Script starting...");

// Fetch initial forecast on startup
log('Fetching initial forecast on startup...');
initOccupancyUrl(function() {
  fetchAndCacheForecast(function(success) {
    if (success) {
      log('Initial forecast cached successfully');
    } else {
      log('Initial forecast fetch failed');
    }
    
    // Start the periodic control loop timer (will skip if not ready)
    Timer.set(CONFIG.pollIntervalMs, true, pollAndControl);
    
    // Try to start control loop if ready
    checkAndStartControlLoop();
    
    log("Script initialization complete");
  });
});

// Schedule daily forecast refresh at midnight
Timer.set(60 * 60 * 1000, true, function() {
  if (shouldRefreshForecast()) {
    log('Daily forecast refresh triggered');
    fetchAndCacheForecast(function(success) {
      if (success) {
        log('Daily forecast refresh successful');
      }
    });
  }
});

// Subscribe to MQTT topics for temperature sources
subscribeMqttTemperatures();

// Handle script stop event
Shelly.addEventHandler(function(eventData) {
  log('Script event:', eventData);
  if (eventData && eventData.info && eventData.info.event === "script_stop") {
    log("Script stopping");
    log('Forecast cache stats: ' + (STATE.cachedForecast ? STATE.cachedForecast.length + ' values' : 'empty') + ', last fetch: ' + STATE.lastForecastFetchDate);
  }
});
