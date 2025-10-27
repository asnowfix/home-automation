// Kalman filter heater controller for Shelly Plus 1 (ES5 style, no Node.js, for Shelly scripting engine)
// Uses Open-Meteo API for weather forecasts (no API key required)

// === STATIC CONSTANTS ===
var SCRIPT_NAME = "heater";

// === STATIC KEYS ===
var CONFIG_KEY_PREFIX = 'script/' + SCRIPT_NAME + '/';
var COOLING_RATE_KEY = CONFIG_KEY_PREFIX + "cooling-rate";
var LAST_CHEAP_END_KEY = CONFIG_KEY_PREFIX + "last-cheap-end";
var OCCUPANCY_URL = 'http://<OCCUPANCY_SENSOR_IP>/status'; // Should return JSON with { "occupied": true/false }

var SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";

var CONFIG = {
  // Configuration values (loaded from KVS or defaults)
  log: true,
  setpoint: 21.0,
  minInternalTemp: 15.0,
  cheapStartHour: 23,
  cheapEndHour: 7,
  pollIntervalMs: 5 * 60 * 1000,
  preheatHours: 2,
  coolingRate: 1.0
};

var CONFIG_KEY = {
  // Configuration keys (to load from KVS)
  log: "log",
  setpoint: "set-point",
  minInternalTemp: "min-internal-temp",
  cheapStartHour: "cheap-start-hour",
  cheapEndHour: "cheap-end-hour",
  pollIntervalMs: "poll-interval-ms",
  preheatHours: "preheat-hours",
  coolingRate: "cooling-rate"
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
  print(SCRIPT_PREFIX, s);
}

// === STATE (DYNAMIC RUNTIME VALUES) ===
var STATE = {
  // MQTT topics (loaded from KV or defaults)
  internalTopic: null,
  externalTopic: null,
  
  // Location
  locationLat: null,
  locationLon: null,
  forecastUrl: null,
  
  // Forecast cache
  cachedForecast: null,
  cachedForecastTimes: null,
  lastForecastFetchDate: null,
  
  // Heater state
  lastHeaterState: false,
  switchedOffValue: 'on'
};

function detectLocationAndLoadConfig() {
  log('Detecting device location...');
  Shelly.call('Shelly.DetectLocation', {}, function(result, error_code, error_message) {
    if (error_code === 0 && result) {
      if (result.lat !== null && result.lon !== null) {
        STATE.locationLat = result.lat;
        STATE.locationLon = result.lon;
        log('Auto-detected location: lat=' + STATE.locationLat + ', lon=' + STATE.locationLon + ', tz=' + result.tz);
        updateForecastURL();
      } else {
        log('Location detection returned null coordinates');
      }
    } else {
      log('Failed to detect location (error ' + error_code + '): ' + error_message);
    }
    // Always proceed to load config after location detection attempt
    loadConfig();
  });
}

function updateForecastURL() {
  if (STATE.locationLat !== null && STATE.locationLon !== null) {
    STATE.forecastUrl = 'https://api.open-meteo.com/v1/forecast?latitude=' + STATE.locationLat + '&longitude=' + STATE.locationLon + '&hourly=temperature_2m&forecast_days=1&timezone=auto';
  }
}

function loadConfigValue(key, defaultValue, callback) {
  if (typeof Shelly !== 'undefined' && Shelly.getKV) {
    Shelly.getKV(CONFIG_KEY_PREFIX + key, function(val) {
      if (val !== null && val !== undefined) {
        // Found value in KVS
        callback(val);
      } else {
        // No value in KV, save default in KVS
        saveConfigValue(key, defaultValue);
        callback(defaultValue);
      }
    });
  } else {
    callback(defaultValue);
  }
}

function saveConfigValue(key, value) {
  if (typeof Shelly !== 'undefined' && Shelly.call) {
    var valueStr = JSON.stringify(value);
    Shelly.call('KVS.Set', { key: CONFIG_KEY_PREFIX + key, value: valueStr }, function(result, error_code, error_msg) {
      if (error_code) {
        log('Error saving config', key, ':', error_msg);
      } else {
        log('Saved config', key, ':', valueStr);
      }
    });
  }
}

function loadConfig() {
  for (var key in CONFIG) {
    if (CONFIG.hasOwnProperty(key)) {
      loadConfigValue(key, CONFIG[key], function(valueStr) {
        var value;
        try {
          value = JSON.parse(valueStr);
        } catch (e) {
          try {
            value = parseFloat(valueStr);
          } catch (e) {
            try {
              value = parseInt(valueStr);
            } catch (e) {
              try {
                value = parseBoolean(valueStr);
              } catch (e) {
                value = valueStr;
              }
            }
          }
        }
        CONFIG[key] = value;
      });
    }
  }
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
    Shelly.setKV(LAST_CHEAP_END_KEY, JSON.stringify({ temp: temp, time: now }));
    log("Stored end-of-cheap-window temp:", temp);
  }
}

function onCheapWindowStart() {
  var data = CONFIG.lastCheapestEnd;
  var prevTemp = data.temp;
  var prevTime = data.time;
  var now = (new Date()).getTime();
  var hours = (now - prevTime) / (3600 * 1000);
  var currTemp = getFilteredTemp();
  if (currTemp !== null && hours > 0) {
    var rate = (prevTemp - currTemp) / hours;
    // Update moving average
    var oldRate = CONFIG.coolingRate;
    var newRate = 0.7 * oldRate + 0.3 * rate; // EMA
    CONFIG.coolingRate = newRate;
    Shelly.setKV(CONFIG_KEY.coolingRate, newRate);
    log("Updated cooling rate:", newRate);
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
  k = CONFIG.coolingRate; // k is now a cooling coefficient (per hour)
  var minutesToEnd = getMinutesToEndOfCheapWindow();
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
  HTTP.request({
    url: OCCUPANCY_URL,
    method: 'GET',
    timeout: 5,
    success: function(resp) {
      var data = null;
      try { data = JSON.parse(resp.body); } catch (e) {}
      cb(data && data.occupied === true);
    },
    error: function(err) {
      log('Error fetching occupancy status:', err);
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

// === MQTT TEMPERATURE HANDLING ===
// Detect if topic is Gen1 or Gen2 format and extract temperature
function parseTemperatureFromMqtt(topic, message) {
  var temp = null;
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
  } catch (e) {
    log('Error parsing temperature from MQTT:', e);
  }
  return null;
}

// Subscribe to MQTT topics for temperature sources
function subscribeMqttTemperatures() {
  if (STATE.internalTopic) {
    log('Subscribing to internal temperature topic:', STATE.internalTopic);
    MQTT.subscribe(STATE.internalTopic, onInternalTemperature, null);
  }
  if (STATE.externalTopic) {
    log('Subscribing to external temperature topic:', STATE.externalTopic);
    MQTT.subscribe(STATE.externalTopic, onExternalTemperature, null);
  }
}

// Callback for internal temperature MQTT messages
function onInternalTemperature(topic, message, userdata) {
  var temp = parseTemperatureFromMqtt(topic, message);
  if (temp !== null) {
    // Store in KVS
    Shelly.call('KVS.Set', { key: 'script/heater/internal', value: JSON.stringify(temp) }, function(result, error_code, error_msg) {
      if (error_code) {
        log('Error storing internal temperature in KVS:', error_msg);
      } else {
        log('Stored internal temperature in KVS:', temp);
      }
    });
  }
}

// Callback for external temperature MQTT messages
function onExternalTemperature(topic, message, userdata) {
  var temp = parseTemperatureFromMqtt(topic, message);
  if (temp !== null) {
    // Store in KVS
    Shelly.call('KVS.Set', { key: 'script/heater/external', value: JSON.stringify(temp) }, function(result, error_code, error_msg) {
      if (error_code) {
        log('Error storing external temperature in KVS:', error_msg);
      } else {
        log('Stored external temperature in KVS:', temp);
      }
    });
  }
}

// === DATA FETCHING FUNCTIONS ===
// Read temperature from KVS (stored by MQTT callbacks)
function getShellyTemperature(location, cb) {
  var key = 'script/heater/' + location;
  Shelly.call('KVS.Get', { key: key }, function(result, error_code, error_msg) {
    if (error_code) {
      log('Error reading temperature from KVS:', location, error_msg);
      cb(null);
    } else if (result && result.value !== undefined) {
      try {
        var temp = JSON.parse(result.value);
        cb(temp);
      } catch (e) {
        log('Error parsing temperature from KVS:', location, e);
        cb(null);
      }
    } else {
      cb(null);
    }
  });
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
  if (!STATE.forecastUrl) {
    log('Open-Meteo forecast URL not configured. Skipping forecast.');
    cb(null);
    return;
  }
  
  log('Fetching fresh forecast from Open-Meteo...');
  HTTP.request({
    url: STATE.forecastUrl,
    method: 'GET',
    timeout: 10,
    success: function(resp) {
      var data = null;
      try { data = JSON.parse(resp.body); } catch (e) {}
      
      if (data && data.hourly && data.hourly.temperature_2m && data.hourly.temperature_2m.length > 0) {
        // Cache the full forecast arrays
        STATE.cachedForecast = data.hourly.temperature_2m;
        STATE.cachedForecastTimes = data.hourly.time;
        var now = new Date();
        STATE.lastForecastFetchDate = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
        log('Cached forecast with ' + STATE.cachedForecast.length + ' hourly values for date: ' + STATE.lastForecastFetchDate);
        cb(true);
      } else {
        log('Failed to parse forecast data');
        cb(false);
      }
    },
    error: function(err) {
      log('Error fetching Open-Meteo forecast:', err);
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

// === PARALLEL DATA FETCH HELPERS (reduce callback nesting) ===
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
    STATE.lastHeaterState = true;
    return;
  }
  shouldPreheat(filteredTemp, forecastTemp, mfTemp, function(preheat) {
    if ((heaterShouldBeOn && isCheapHour()) || preheat) {
      log('Heater ON (normal or preheat mode)', 'preheat:', preheat);
      setHeaterState(true);
      STATE.lastHeaterState = true;
    } else {
      log('Outside cheap window => no heating');
      setHeaterState(false);
      STATE.lastHeaterState = false;
    }
  });
}

// === MAIN CONTROL LOOP (flattened) ===
function pollAndControl() {
  fetchAllControlInputs(controlHeaterWithInputs);
}

// === SWITCHED-OFF VALUE SUPPORT ===
function loadSwitchedOffValue() {
  if (typeof Shelly !== 'undefined' && Shelly.getComponentConfig) {
    Shelly.getComponentConfig('Switch', function(cfg) {
      if (cfg && cfg.kvs && cfg.kvs['switched-off']) {
        STATE.switchedOffValue = cfg.kvs['switched-off'];
        log('Loaded switched-off value from KV:', STATE.switchedOffValue);
      } else {
        log('No switched-off value in KV, using default:', STATE.switchedOffValue);
      }
    });
  } else {
    log('Shelly.getComponentConfig not available, using default switched-off value:', STATE.switchedOffValue);
  }
}

// Call this once at script start
loadSwitchedOffValue();

// === HEATER CONTROL (LOCAL SHELLY CALL, SUPPORTS switched-off VALUE) ===
function setHeaterState(on) {
  if (on) {
    Shelly.call("Switch.Set", { id: 0, on: true }, function(result, error_code, error_msg) {
      if (error_code) {
        log('Error setting heater state:', error_msg);
      } else {
        log('Heater relay set to ON');
      }
    });
  } else {
    // Use the cached switched-off value
    Shelly.call("Switch.Set", { id: 0, on: false, value: STATE.switchedOffValue }, function(result, error_code, error_msg) {
      if (error_code) {
        log('Error setting heater state:', error_msg);
      } else {
        log('Heater relay set to OFF (value:', STATE.switchedOffValue, ')');
      }
    });
  }
}

// === SCHEDULED EXECUTION ===
log("Script starting...");

// Fetch initial forecast on startup
log('Fetching initial forecast on startup...');
fetchAndCacheForecast(function(success) {
  if (success) {
    log('Initial forecast cached successfully');
  } else {
    log('Initial forecast fetch failed');
  }
  
  // Start the control loop
  Timer.set(CONFIG.pollIntervalMs, true, pollAndControl);
  
  // Initial run
  pollAndControl();
  
  log("Script initialization complete");
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

// Handle script stop event
Shelly.addEventHandler(function(eventData) {
  if (eventData && eventData.info && eventData.info.event === "script_stop") {
    log("Script stopping");
    log('Forecast cache stats: ' + (STATE.cachedForecast ? STATE.cachedForecast.length + ' values' : 'empty') + ', last fetch: ' + STATE.lastForecastFetchDate);
  }
});
