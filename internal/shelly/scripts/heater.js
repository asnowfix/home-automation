// Kalman filter heater controller for Shelly Plus 1 (ES5 style, no Node.js, for Shelly scripting engine)
// Fill in your URLs and AccuWeather credentials below

var CONFIG = {
  script: "[heater] ",
  log: true
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

// === CONFIGURATION (DYNAMIC FROM KV) ===
var CONFIG_KV_KEY = 'script/heater/config';

// Defaults (will be overwritten by KV if present)
var INTERNAL_THERMOMETER_TOPIC = null; // MQTT topic for internal temperature
var EXTERNAL_THERMOMETER_TOPIC = null; // MQTT topic for external temperature
var SETPOINT = 21.0; // Desired indoor temperature in Celsius
var MIN_INTERNAL_TEMP = 15.0; // If filtered internal temp drops below this, always heat
var CHEAP_START_HOUR = 23; // 23:00
var CHEAP_END_HOUR = 7;    // 7:00
var POLL_INTERVAL_MS = 5 * 60 * 1000; // 5 minutes
var ACCUWEATHER_API_KEY = '<YOUR_ACCUWEATHER_API_KEY>';
var ACCUWEATHER_LOCATION_KEY = '<YOUR_LOCATION_KEY>';
var ACCUWEATHER_FORECAST_URL = 'http://dataservice.accuweather.com/forecasts/v1/hourly/1hour/' + ACCUWEATHER_LOCATION_KEY + '?apikey=' + ACCUWEATHER_API_KEY + '&metric=true';
var METEOFRANCE_API_KEY = '<YOUR_METEOFRANCE_API_KEY>';
var METEOFRANCE_LAT = '<YOUR_LATITUDE>';
var METEOFRANCE_LON = '<YOUR_LONGITUDE>';
var METEOFRANCE_FORECAST_URL = 'https://api.meteofrance.com/public/forecast?lat=' + METEOFRANCE_LAT + '&lon=' + METEOFRANCE_LON;
var PREHEAT_HOURS = 2;

function loadHeaterConfigFromKV() {
  if (typeof Shelly !== 'undefined' && Shelly.getKV) {
    Shelly.getKV(CONFIG_KV_KEY, function(val) {
      if (val) {
        try {
          var cfg = JSON.parse(val);
          if (cfg.internal_topic) INTERNAL_THERMOMETER_TOPIC = cfg.internal_topic;
          if (cfg.external_topic) EXTERNAL_THERMOMETER_TOPIC = cfg.external_topic;
          if (cfg.setpoint !== undefined) SETPOINT = cfg.setpoint;
          if (cfg.min_temp !== undefined) MIN_INTERNAL_TEMP = cfg.min_temp;
          if (cfg.cheap_start !== undefined) CHEAP_START_HOUR = cfg.cheap_start;
          if (cfg.cheap_end !== undefined) CHEAP_END_HOUR = cfg.cheap_end;
          if (cfg.poll_interval_ms !== undefined) POLL_INTERVAL_MS = cfg.poll_interval_ms;
          if (cfg.accuweather_api_key) ACCUWEATHER_API_KEY = cfg.accuweather_api_key;
          if (cfg.accuweather_location_key) ACCUWEATHER_LOCATION_KEY = cfg.accuweather_location_key;
          if (cfg.meteofrance_api_key) METEOFRANCE_API_KEY = cfg.meteofrance_api_key;
          if (cfg.meteofrance_lat) METEOFRANCE_LAT = cfg.meteofrance_lat;
          if (cfg.meteofrance_lon) METEOFRANCE_LON = cfg.meteofrance_lon;
          if (cfg.preheat_hours !== undefined) PREHEAT_HOURS = cfg.preheat_hours;
          // Update forecast URLs if any of the above changed
          ACCUWEATHER_FORECAST_URL = 'http://dataservice.accuweather.com/forecasts/v1/hourly/1hour/' + ACCUWEATHER_LOCATION_KEY + '?apikey=' + ACCUWEATHER_API_KEY + '&metric=true;';
          METEOFRANCE_FORECAST_URL = 'https://api.meteofrance.com/public/forecast?lat=' + METEOFRANCE_LAT + '&lon=' + METEOFRANCE_LON;
          log('Loaded heater config from KV:', val);
          // Subscribe to MQTT topics after config is loaded
          subscribeMqttTemperatures();
        } catch (e) {
          log('Error parsing heater config KV:', e);
        }
      } else {
        log('No heater config in KV, using defaults');
        subscribeMqttTemperatures();
      }
    });
  } else {
    log('Shelly.getKV not available, using default config');
    subscribeMqttTemperatures();
  }
}

// Call this once at script start
loadHeaterConfigFromKV();

// === SAFETY MINIMUM TEMPERATURE ===

// === OCCUPANCY CONFIGURATION ===
var OCCUPANCY_URL = 'http://<OCCUPANCY_SENSOR_IP>/status'; // Should return JSON with { "occupied": true/false }

// === TIME WINDOW FOR HEATING ===
// Set your cheap electricity window here (24h format)

function isCheapHour() {
  var now = new Date();
  var hour = now.getHours();
  return (hour >= CHEAP_START_HOUR || hour < CHEAP_END_HOUR);
}

// === COOLING RATE LEARNING (AUTOMATIC) ===
var COOLING_RATE_KEY = "script/heater/cooling-rate";
var LAST_CHEAP_END_KEY = "script/heater/last-cheap-end";
var COOLING_RATE_DEFAULT = 1.0;

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
  Shelly.getKV(LAST_CHEAP_END_KEY, function(val) {
    if (!val) return;
    var data = JSON.parse(val);
    var prevTemp = data.temp;
    var prevTime = data.time;
    var now = (new Date()).getTime();
    var hours = (now - prevTime) / (3600 * 1000);
    var currTemp = getFilteredTemp();
    if (currTemp !== null && hours > 0) {
      var rate = (prevTemp - currTemp) / hours;
      // Update moving average
      Shelly.getKV(COOLING_RATE_KEY, function(oldVal) {
        var oldRate = oldVal ? parseFloat(oldVal) : COOLING_RATE_DEFAULT;
        var newRate = 0.7 * oldRate + 0.3 * rate; // EMA
        Shelly.setKV(COOLING_RATE_KEY, newRate);
        log("Updated cooling rate:", newRate);
      });
    }
  });
}

function getCoolingRate(cb) {
  Shelly.getKV(COOLING_RATE_KEY, function(val) {
    cb(val ? parseFloat(val) : COOLING_RATE_DEFAULT);
  });
}

// Schedule learning events at CHEAP_START_HOUR and CHEAP_END_HOUR
function scheduleLearningTimers() {
  var now = new Date();
  var hour = now.getHours();
  var minute = now.getMinutes();
  var second = now.getSeconds();
  var msNow = hour * 3600000 + minute * 60000 + second * 1000;
  var scheduleAt = function(targetHour, cb) {
    var msTarget = targetHour * 3600000;
    if (msTarget <= msNow) msTarget += 24 * 3600000; // next day
    var delay = msTarget - msNow;
    Timer.set(delay, false, function() {
      cb();
      // Reschedule for next day
      Timer.set(24 * 3600000, true, cb);
    });
  };
  scheduleAt(CHEAP_END_HOUR, onCheapWindowEnd);
  scheduleAt(CHEAP_START_HOUR, onCheapWindowStart);
}

// Call this once at script start
scheduleLearningTimers();

// === PRE-HEATING CONFIGURATION ===
// How many hours before the end of the cheap window to start pre-heating (if needed)
var PREHEAT_HOURS = 2;

// Estimate how fast your home cools down (degC/hour)
// (will be learned, but keep a default fallback)
var COOLING_RATE = COOLING_RATE_DEFAULT;

function getMinutesToEndOfCheapWindow() {
  var now = new Date();
  var hour = now.getHours();
  var minute = now.getMinutes();
  var end = CHEAP_END_HOUR;
  var minutesNow = hour * 60 + minute;
  var minutesEnd = end * 60;
  if (end <= CHEAP_START_HOUR) minutesEnd += 24 * 60; // handle overnight windows
  if (minutesNow > minutesEnd) minutesEnd += 24 * 60; // handle wrap-around
  return minutesEnd - minutesNow;
}

// === ADVANCED COOLING MODEL: LOSS DEPENDS ON TEMP DIFFERENCE ===
// We now use: predictedDrop = COOLING_COEFF * (filteredTemp - externalTemp) * hoursToEnd
// COOLING_COEFF is learned as before (from data)

function shouldPreheat(filteredTemp, forecastTemp, mfTemp, cb) {
  getCoolingRate(function(k) { // k is now a cooling coefficient (per hour)
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
    cb((hoursToEnd <= PREHEAT_HOURS) && (predictedTemp < SETPOINT));
  });
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
  if (INTERNAL_THERMOMETER_TOPIC) {
    log('Subscribing to internal temperature topic:', INTERNAL_THERMOMETER_TOPIC);
    MQTT.subscribe(INTERNAL_THERMOMETER_TOPIC, onInternalTemperature, null);
  }
  if (EXTERNAL_THERMOMETER_TOPIC) {
    log('Subscribing to external temperature topic:', EXTERNAL_THERMOMETER_TOPIC);
    MQTT.subscribe(EXTERNAL_THERMOMETER_TOPIC, onExternalTemperature, null);
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

function getAccuWeatherForecast(cb) {
  if (!ACCUWEATHER_API_KEY || !ACCUWEATHER_LOCATION_KEY) {
    log('AccuWeather API key or location key missing. Skipping forecast.');
    cb(null);
    return;
  }
  HTTP.request({
    url: ACCUWEATHER_FORECAST_URL,
    method: 'GET',
    timeout: 10,
    success: function(resp) {
      var data = null;
      try { data = JSON.parse(resp.body); } catch (e) {}
      var temp = null;
      if (data && data.length > 0 && data[0].Temperature && typeof data[0].Temperature.Value !== 'undefined') {
        temp = data[0].Temperature.Value;
      }
      cb(temp);
    },
    error: function(err) {
      log('Error fetching AccuWeather forecast:', err);
      cb(null);
    }
  });
}

function getMeteoFranceForecast(cb) {
  if (!METEOFRANCE_API_KEY || !METEOFRANCE_LAT || !METEOFRANCE_LON) {
    log('MeteoFrance API key or location missing. Skipping forecast.');
    cb(null);
    return;
  }
  HTTP.request({
    url: METEOFRANCE_FORECAST_URL,
    method: 'GET',
    timeout: 10,
    headers: { 'apikey': METEOFRANCE_API_KEY },
    success: function(resp) {
      var data = null;
      try { data = JSON.parse(resp.body); } catch (e) {}
      var temp = null;
      // Try to get the next planned temperature (1 hour ahead)
      if (data && data.forecast && data.forecast.length > 0 && typeof data.forecast[0].T !== 'undefined') {
        temp = data.forecast[0].T;
      }
      cb(temp);
    },
    error: function(err) {
      log('Error fetching MeteoFrance forecast:', err);
      cb(null);
    }
  });
}

// === PARALLEL DATA FETCH HELPERS (reduce callback nesting) ===
function fetchAllControlInputs(cb) {
  var results = { internal: null, external: null, forecast: null, mf: null, occupied: null };
  var done = 0;
  var total = 5;
  function check() {
    done++;
    if (done === total) cb(results);
  }
  getShellyTemperature('internal', function(val) { results.internal = val; check(); });
  getShellyTemperature('external', function(val) { results.external = val; check(); });
  getAccuWeatherForecast(function(val) { results.forecast = val; check(); });
  getMeteoFranceForecast(function(val) { results.mf = val; check(); });
  getOccupancy(function(val) { results.occupied = val; check(); });
}

function controlHeaterWithInputs(results) {
  var internalTemp = results.internal;
  var externalTemp = results.external;
  var forecastTemp = results.forecast;
  var mfTemp = results.mf;
  var isOccupied = results.occupied;
  log('Internal:', internalTemp, 'External:', externalTemp, 'AccuWeather:', forecastTemp, 'MeteoFrance:', mfTemp, 'Occupied:', isOccupied);
  if (internalTemp === null) {
    log('Skipping control cycle due to missing internal temperature');
    return;
  }
  var controlInput = 0;
  var count = 0;
  if (externalTemp !== null) { controlInput += externalTemp; count++; }
  if (forecastTemp !== null) { controlInput += forecastTemp; count++; }
  if (mfTemp !== null) { controlInput += mfTemp; count++; }
  if (count > 0) controlInput = controlInput / count;
  var filteredTemp = kalman.filter(internalTemp, controlInput);
  log('Filtered temperature:', filteredTemp);
  var heaterShouldBeOn = filteredTemp < SETPOINT;
  // SAFETY: If filtered temperature is below MIN_INTERNAL_TEMP, always heat IF occupied
  if (isOccupied && filteredTemp < MIN_INTERNAL_TEMP) {
    log('Safety override: internal temp', filteredTemp, 'below MIN_INTERNAL_TEMP', MIN_INTERNAL_TEMP, '=> HEAT');
    setHeaterState(true);
    lastHeaterState = true;
    return;
  }
  shouldPreheat(filteredTemp, forecastTemp, mfTemp, function(preheat) {
    if ((heaterShouldBeOn && isCheapHour()) || preheat) {
      log('Heater ON (normal or preheat mode)', 'preheat:', preheat);
      setHeaterState(true);
      lastHeaterState = true;
    } else {
      log('Outside cheap window => no heating');
      setHeaterState(false);
      lastHeaterState = false;
    }
  });
}

// === MAIN CONTROL LOOP (flattened) ===
function pollAndControl() {
  fetchAllControlInputs(controlHeaterWithInputs);
}

// === SWITCHED-OFF VALUE SUPPORT ===
// If a group/device KV contains a "switched-off" key, use its value for relay OFF
var SWITCHED_OFF_VALUE = 'on'; // Default value when Shelly Plus 1 one/freeze cabling

function loadSwitchedOffValue() {
  if (typeof Shelly !== 'undefined' && Shelly.getComponentConfig) {
    Shelly.getComponentConfig('Switch', function(cfg) {
      if (cfg && cfg.kvs && cfg.kvs['switched-off']) {
        SWITCHED_OFF_VALUE = cfg.kvs['switched-off'];
        log('Loaded switched-off value from KV:', SWITCHED_OFF_VALUE);
      } else {
        log('No switched-off value in KV, using default:', SWITCHED_OFF_VALUE);
      }
    });
  } else {
    log('Shelly.getComponentConfig not available, using default switched-off value:', SWITCHED_OFF_VALUE);
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
    Shelly.call("Switch.Set", { id: 0, on: false, value: SWITCHED_OFF_VALUE }, function(result, error_code, error_msg) {
      if (error_code) {
        log('Error setting heater state:', error_msg);
      } else {
        log('Heater relay set to OFF (value:', SWITCHED_OFF_VALUE, ')');
      }
    });
  }
}

// === SCHEDULED EXECUTION ===
log("Script starting...");

Timer.set(POLL_INTERVAL_MS, true, pollAndControl);

// Initial run
pollAndControl();

log("Script initialization complete");

// Handle script stop event
Shelly.addEventHandler(function(eventData) {
  if (eventData && eventData.info && eventData.info.event === "script_stop") {
    log("Script stopping");
  }
});
