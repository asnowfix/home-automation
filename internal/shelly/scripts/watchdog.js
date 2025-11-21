// watchdog.js - Combined watchdog script for Shelly devices
// This script combines multiple functionalities:
// 1. MQTT watchdog: Monitors MQTT connection and reboots if connection fails repeatedly
// 2. Firmware updater: Checks for firmware updates weekly and applies them automatically
// 3. Prometheus metrics: Exposes device metrics in Prometheus format via HTTP endpoint

// Shared state and configuration for all components
var SHARED_STATE = {
    rebootLock: false,  // When true, prevents other components from triggering reboots
    rebootLockReason: "" // Reason for the reboot lock
};

// Shared configuration for all components
var CONFIG = {
    // Script settings
    scriptName: "watchdog",
    enableLogging: true,
    // MQTT Watchdog settings
    mqtt: {
        numberOfFails: 5,
        retryIntervalSeconds: 10
    },
    // Firmware Update settings
    firmwareUpdate: {
        checkIntervalDays: 7,  // Check for updates every 7 days
        updateChannel: "stable", // Use "stable" or "beta"
        autoUpdate: true       // Whether to automatically apply updates
    },
    // Prometheus metrics settings
    prometheus: {
        enabled: true,
        publishIntervalSeconds: 30,  // Publish metrics every 30 seconds
        mqttTopic: "shelly/metrics",  // MQTT topic for metrics
        monitoredSwitches: ["switch:0"]
    }
};

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
  print(CONFIG.scriptName + ": " + s);
}

// Use namespaces to avoid variable/function conflicts
var MqttWatchdog = {
    
    failCounter: 0,
    timer: null,
    
    // Helper function for logging
    log: log.bind(this, "[MqttWatchdog]"),
    
    // Attempt to reconnect MQTT by rebooting the device
    attemptReconnect: function() {
        // Check if reboot is locked
        if (SHARED_STATE.rebootLock) {
            this.log("Reboot prevented: " + SHARED_STATE.rebootLockReason);
            return;
        }
        
        this.log("Attempting to reconnect MQTT by rebooting device");
        Shelly.call("Shelly.Reboot", {});
    },
    
    // Check if MQTT is enabled in configuration
    isMqttEnabled: function() {
        var mqttConfig = Shelly.getComponentConfig("mqtt");
        return mqttConfig && mqttConfig.enable === true;
    },
    
    // Check if MQTT is connected
    isMqttConnected: function() {
        return MQTT.isConnected();
    },
    
    // Regular check function that runs on a timer
    checkMqttConnection: function() {
        if (!this.isMqttEnabled()) {
            this.log("MQTT is not enabled in configuration");
            return;
        }
        
        if (this.isMqttConnected()) {
            if (this.failCounter > 0) {
                this.log("MQTT connection restored after " + this.failCounter + " failures");
            }
            this.failCounter = 0;
        } else {
            this.failCounter++;
            this.log("MQTT connection check failed: " + this.failCounter + "/" + CONFIG.mqtt.numberOfFails);
            
            if (this.failCounter >= CONFIG.mqtt.numberOfFails) {
                this.log("Reached maximum failures, rebooting device");
                this.attemptReconnect();
                return; // Don't schedule another check since we're rebooting
            }
        }
        
        // Schedule the next check
        var self = this;
        this.timer = Timer.set(CONFIG.mqtt.retryIntervalSeconds * 1000, false, function() {
            self.checkMqttConnection();
        });
    },
    
    // Initialize the MQTT watchdog
    init: function() {
        if (this.isMqttEnabled()) {
            this.log("Starting");
            this.checkMqttConnection();
        } else {
            this.log("MQTT is not enabled, skipping watchdog");
        }
    }
};

// Firmware Update module
var FirmwareUpdater = {
    updateTimer: null,
    lastCheckTimestamp: 0,
    
    // Helper function for logging
    log: log.bind(this, "[FirmwareUpdater]"),
    
    // Check if firmware update is available
    checkForUpdate: function() {
        this.log("Checking for firmware updates...");
        
        // Record the current check time
        this.lastCheckTimestamp = Date.now();
        
        // Call the Shelly API to check for updates
        Shelly.call("Shelly.CheckForUpdate", {}, function(result, error_code, error_message) {
            if (error_code) {
                this.log("Error checking for updates: " + error_message);
                return;
            }
            
            // If result is empty, no update is available
            if (!result || (Object.keys(result).length === 0)) {
                this.log("No firmware updates available");
                return;
            }
            
            // Determine which update to use based on configuration
            var updateInfo = null;
            if (CONFIG.firmwareUpdate.updateChannel === "beta" && result.beta) {
                updateInfo = result.beta;
                this.log("Beta update available: " + updateInfo.version + " (" + updateInfo.build_id + ")");
            } else if (result.stable) {
                updateInfo = result.stable;
                this.log("Stable update available: " + updateInfo.version + " (" + updateInfo.build_id + ")");
            }
            
            // If update is available and auto-update is enabled, apply it
            if (updateInfo && CONFIG.firmwareUpdate.autoUpdate) {
                this.applyUpdate();
            }
        }.bind(this));
    },
    
    // Apply the firmware update
    applyUpdate: function() {
        this.log("Applying firmware update...");
        
        // Set the reboot lock to prevent other components from rebooting
        SHARED_STATE.rebootLock = true;
        SHARED_STATE.rebootLockReason = "Firmware update in progress";
        this.log("Reboot lock enabled: " + SHARED_STATE.rebootLockReason);
        
        // Call the Shelly API to apply the update
        Shelly.call("Shelly.Update", { stage: CONFIG.firmwareUpdate.updateChannel }, function(result, error_code, error_message) {
            if (error_code) {
                this.log("Error applying update: " + error_message);
                return;
            }
            
            this.log("Update initiated successfully. Device will reboot.");
        }.bind(this));
    },
    
    // Schedule the next update check
    scheduleNextCheck: function() {
        // Calculate milliseconds until next check (CONFIG.firmwareUpdate.checkIntervalDays days)
        var checkIntervalMs = CONFIG.firmwareUpdate.checkIntervalDays * 24 * 60 * 60 * 1000;
        
        // Clear any existing timer
        if (this.updateTimer !== null) {
            Timer.clear(this.updateTimer);
        }
        
        // Use recurring timer instead of recursive callback to prevent closure chain buildup
        var self = this;
        this.updateTimer = Timer.set(checkIntervalMs, true, function() {
            self.checkForUpdate();
            // Don't call scheduleNextCheck() here - recurring timer handles it
        });
        
        this.log("Next firmware check scheduled in " + CONFIG.firmwareUpdate.checkIntervalDays + " days");
    },
    
    // Initialize the firmware updater
    init: function() {
        this.log("Initializing firmware update checker");
        
        // Perform initial check
        this.checkForUpdate();
        
        // Schedule regular checks
        this.scheduleNextCheck();
    }
};

// Prometheus Metrics module
var PrometheusMetrics = {
    // Constants
    TYPE_GAUGE: "gauge",
    TYPE_COUNTER: "counter",
    
    // Device info
    deviceInfo: null,
    defaultLabelsStr: "",
    metricPrefix: "shelly_",
    emittedMeta: {},
    publishTimer: null,
    
    // Helper function for logging
    log: log.bind(this, "[PrometheusMetrics]"),
    
    // Initialize Prometheus metrics
    init: function() {
        this.log("Initializing metrics");
        
        if (!CONFIG.prometheus || !CONFIG.prometheus.enabled) {
            this.log("Prometheus metrics are disabled in configuration");
            return;
        }
        
        try {
            // Get device info
            this.deviceInfo = Shelly.getDeviceInfo();
            
            // Build default labels string directly without intermediate arrays
            this.defaultLabelsStr = this.promLabel("name", this.deviceInfo.name) + "," +
                                   this.promLabel("id", this.deviceInfo.id) + "," +
                                   this.promLabel("mac", this.deviceInfo.mac) + "," +
                                   this.promLabel("app", this.deviceInfo.app);
            this.emittedMeta = {};

            // Start periodic MQTT publishing
            var intervalMs = CONFIG.prometheus.publishIntervalSeconds * 1000;
            this.log("Starting metrics publisher (interval: " + CONFIG.prometheus.publishIntervalSeconds + "s)");
            
            var self = this;
            this.publishTimer = Timer.set(intervalMs, true, function() {
                self.publishMetrics();
            });
            
            // Publish immediately on startup
            this.publishMetrics();
            
        } catch (e) {
            this.log("Error while initializing Prometheus metrics: " + e.message);
        }

        this.log("Metrics initialized");
    },
    
    // Create a Prometheus label
    promLabel: function(label, value) {
        return [label, "=", '"', value, '"'].join("");
    },

    // Generate one metric using string concatenation (no arrays)
    printPrometheusMetric: function(name, type, specificLabels, description, value) {
        // Build labels string with precomputed default labels
        var labels = this.defaultLabelsStr;
        if (specificLabels && specificLabels.length > 0) {
            labels = labels + "," + specificLabels.join(",");
        }

        var result = "";
        // Emit HELP/TYPE once per metric family
        if (!this.emittedMeta[name]) {
            result += "# HELP " + this.metricPrefix + name + " " + description + "\n";
            result += "# TYPE " + this.metricPrefix + name + " " + type + "\n";
            this.emittedMeta[name] = true;
        }

        result += this.metricPrefix + name + "{" + labels + "} " + String(value) + "\n\n";
        return result;
    },
    
    // Publish metrics to MQTT
    publishMetrics: function() {
        if (!MQTT.isConnected()) {
            this.log("MQTT not connected, skipping metrics publish");
            return;
        }
        
        try {
            // Reset meta registry for fresh HELP/TYPE emission
            this.emittedMeta = {};
            
            // Generate metrics
            var metrics = this.generateMetricsForSystem() + this.generateMetricsForSwitches();
            
            // Publish to MQTT topic
            var topic = CONFIG.prometheus.mqttTopic + "/" + this.deviceInfo.id;
            MQTT.publish(topic, metrics, 0, false);
            
            this.log("Published metrics to " + topic);
        } catch (e) {
            this.log("Error publishing metrics: " + e.message);
            if (e && false) {}  // Prevent minifier from removing catch parameter
        }
    },
    
    // Generate metrics for the system
    generateMetricsForSystem: function() {
        var sys = Shelly.getComponentStatus("sys");
        var result = "";
        result += this.printPrometheusMetric("uptime_seconds", this.TYPE_COUNTER, [], "System uptime in seconds", sys.uptime);
        result += this.printPrometheusMetric("ram_size_bytes", this.TYPE_GAUGE, [], "Internal board RAM size in bytes", sys.ram_size);
        result += this.printPrometheusMetric("ram_free_bytes", this.TYPE_GAUGE, [], "Internal board free RAM size in bytes", sys.ram_free);
        return result;
    },
    
    // Generate metrics for all monitored switches
    generateMetricsForSwitches: function() {
        var list = CONFIG.prometheus.monitoredSwitches && CONFIG.prometheus.monitoredSwitches.length > 0 ? CONFIG.prometheus.monitoredSwitches : [];
        var result = "";
        for (var i = 0; i < list.length; i++) {
            result += this.generateMetricsForSwitch(list[i]);
        }
        return result;
    },
    
    // Generate metrics for a specific switch
    generateMetricsForSwitch: function(id) {
        try {
            var stringId = "switch:" + id;
            var sw = Shelly.getComponentStatus(stringId);
            if (!sw) {
                return "";
            }
            
            var switchLabel = this.promLabel("switch", stringId);
            
            var result = "";
            result += this.printPrometheusMetric("switch_power_watts", this.TYPE_GAUGE, [switchLabel], "Instant power consumption in watts", sw.apower || 0);
            result += this.printPrometheusMetric("switch_voltage_volts", this.TYPE_GAUGE, [switchLabel], "Instant voltage in volts", sw.voltage || 0);
            result += this.printPrometheusMetric("switch_current_amperes", this.TYPE_GAUGE, [switchLabel], "Instant current in amperes", sw.current || 0);
            result += this.printPrometheusMetric("switch_temperature_celsius", this.TYPE_GAUGE, [switchLabel], "Temperature of the device in celsius", sw.temperature && sw.temperature.tC ? sw.temperature.tC : 0);
            result += this.printPrometheusMetric("switch_power_total", this.TYPE_COUNTER, [switchLabel], "Accumulated energy consumed in watts hours", sw.aenergy && sw.aenergy.total ? sw.aenergy.total : 0);
            result += this.printPrometheusMetric("switch_output", this.TYPE_GAUGE, [switchLabel], "Switch state (1=on, 0=off)", sw.output ? 1 : 0);
            return result;
        } catch (e) {
            if (e && false) {}  // Prevent minifier from removing catch parameter
            return "";
        }
    }
};

// Initialize all components (wrapped to prevent minifier collapsing into comma sequences)
(function() {
    print("Script starting...");
    
    MqttWatchdog.init();
    FirmwareUpdater.init();
    PrometheusMetrics.init();
    
    print("Script startup complete");
})();
