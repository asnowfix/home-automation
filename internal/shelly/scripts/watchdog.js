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
        numberOfFails: 5,           // 5 retries connceting to the server before rebooting the device
        retryIntervalSeconds: 60    // Retry every 60 seconds
    },
    // Firmware Update settings
    firmwareUpdate: {
        checkIntervalDays: 7,  // Check for updates every 7 days
        updateChannel: "stable", // Use "stable" or "beta"
        autoUpdate: true       // Whether to automatically apply updates
    },
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
        }

        // Check outside the else block so the minifier cannot collapse it into
        // `else if(stmt1, stmt2, cond)` — a comma expression in an if() condition
        // that Espruino's parser rejects with a syntax error.
        if (this.failCounter >= CONFIG.mqtt.numberOfFails) {
            this.log("Reached maximum failures, rebooting device");
            this.attemptReconnect();
            return; // Don't schedule another check since we're rebooting
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
        var self = this;
        Shelly.call("Shelly.CheckForUpdate", {}, function(result, error_code, error_message) {
            if (error_code) {
                self.log("Error checking for updates: " + error_message);
                return;
            }

            // If result is empty, no update is available
            if (!result || (Object.keys(result).length === 0)) {
                self.log("No firmware updates available");
                return;
            }

            // Determine which update to use based on configuration
            var updateInfo = null;
            if (CONFIG.firmwareUpdate.updateChannel === "beta" && result.beta) {
                updateInfo = result.beta;
                self.log("Beta update available: " + updateInfo.version + " (" + updateInfo.build_id + ")");
            } else if (result.stable) {
                updateInfo = result.stable;
                self.log("Stable update available: " + updateInfo.version + " (" + updateInfo.build_id + ")");
            }

            // If update is available and auto-update is enabled, apply it
            if (updateInfo && CONFIG.firmwareUpdate.autoUpdate) {
                self.applyUpdate();
            }
        });
    },
    
    // Apply the firmware update
    applyUpdate: function() {
        this.log("Applying firmware update...");
        
        // Set the reboot lock to prevent other components from rebooting
        SHARED_STATE.rebootLock = true;
        SHARED_STATE.rebootLockReason = "Firmware update in progress";
        this.log("Reboot lock enabled: " + SHARED_STATE.rebootLockReason);
        
        // Call the Shelly API to apply the update
        var self = this;
        Shelly.call("Shelly.Update", { stage: CONFIG.firmwareUpdate.updateChannel }, function(result, error_code, error_message) {
            if (error_code) {
                self.log("Error applying update: " + error_message);
                return;
            }


            self.log("Update initiated successfully. Device will reboot.");
        });
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

// Initialize all components (wrapped to prevent minifier collapsing into comma sequences)
(function() {
    print("Script starting...");

    MqttWatchdog.init();
    FirmwareUpdater.init();

    print("Script startup complete");
})();
