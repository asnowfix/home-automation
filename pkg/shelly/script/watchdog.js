// watchdog.js - Combined watchdog script for Shelly devices
// This script combines multiple functionalities:
// 1. MQTT watchdog: Monitors MQTT connection and reboots if connection fails repeatedly
// 2. Daily reboot: Schedules a random reboot once per day within a configured time window
// 3. IP Assignment watchdog: Monitors network connectivity and reboots if no IP is assigned
// 4. Firmware updater: Checks for firmware updates weekly and applies them automatically
// 5. Prometheus metrics: Exposes device metrics in Prometheus format via HTTP endpoint

// Shared state and configuration for all components
let SHARED_STATE = {
    rebootLock: false,  // When true, prevents other components from triggering reboots
    rebootLockReason: "", // Reason for the reboot lock
    syslogEnabled: false  // Set to true when Syslog is initialized successfully
};

// Shared configuration for all components
let CONFIG = {
    // Remote logging settings
    logging: {
        enabled: false,           // Set to true to enable remote logging
        method: "mqtt",           // "webhook" or "mqtt"
        url: "http://192.168.1.100:8080/logs", // Webhook URL for HTTP logging
        mqttTopic: "shelly/logs", // MQTT topic for logging if method is "mqtt"
        hostname: "shelly",       // Device hostname in logs
        appName: "watchdog"       // Application name in logs
    },
    // MQTT Watchdog settings
    mqtt: {
        numberOfFails: 5,
        retryIntervalSeconds: 10,
        debug: true
    },
    // Daily Reboot settings
    dailyReboot: {
        windowStartHour: 2,   // Earliest hour to reboot (2 = 2:00 AM)
        windowEndHour: 5      // Latest hour to reboot (5 = 5:59 AM)
    },
    // IP Assignment Watchdog settings
    ipAssignment: {
        numberOfFails: 5,      // Number of failures before triggering a restart
        retryIntervalSeconds: 60 // Time in seconds between retries
    },
    // Firmware Update settings
    firmwareUpdate: {
        checkIntervalDays: 7,  // Check for updates every 7 days
        updateChannel: "stable", // Use "stable" or "beta"
        autoUpdate: true       // Whether to automatically apply updates
    },
    // Global settings
    debug: true,
    // Prometheus metrics settings
    prometheus: {
        enabled: true,
        endpoint: "metrics",
        monitoredSwitches: ["switch:0"]
    }
};

// Use namespaces to avoid variable/function conflicts
let MqttWatchdog = {
    
    failCounter: 0,
    timer: null,
    
    // Helper function for logging
    log: function(message) {
        if (CONFIG.debug) {
            print("[MqttWatchdog] " + message);
        }
    },
    
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
        let mqttConfig = Shelly.getComponentConfig("mqtt");
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
        let self = this;
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

let DailyReboot = {
    // No local configuration - using shared CONFIG object
    
    // === INTERNAL STATE ===
    rebootScheduled: false,
    
    // Helper function for logging
    log: function(message) {
        if (CONFIG.debug) {
            print("[DailyReboot] " + message);
        }
    },
    
    getRandomInt: function(min, max) {
        // Inclusive min, exclusive max
        return Math.floor(Math.random() * (max - min)) + min;
    },
    
    scheduleRandomReboot: function() {
        // Get current date/time
        let now = new Date();
        let tomorrow = new Date(now.getTime() + 24*60*60*1000);
        // Pick a random hour/minute in the window
        let hour = this.getRandomInt(CONFIG.dailyReboot.windowStartHour, CONFIG.dailyReboot.windowEndHour + 1);
        let minute = this.getRandomInt(0, 60);
        let target = new Date(tomorrow.getFullYear(), tomorrow.getMonth(), tomorrow.getDate(), hour, minute, 0, 0);
        let delayMs = target.getTime() - now.getTime();
        this.log("Scheduling next reboot at " + target.toISOString());
        
        let self = this;
        Timer.set(delayMs, false, function() {
            // Check if reboot is locked
            if (SHARED_STATE.rebootLock) {
                self.log("Scheduled reboot prevented: " + SHARED_STATE.rebootLockReason);
                // Reschedule for tomorrow
                self.scheduleRandomReboot();
                return;
            }
            
            self.log("Rebooting device now...");
            Shelly.call("Sys.Reboot", null, null);
            // After reboot, reschedule for the next day
            self.scheduleRandomReboot();
        });
    },
    
    // Initialize the daily reboot scheduler
    init: function() {
        if (!this.rebootScheduled) {
            this.log("Initializing daily reboot scheduler");
            this.scheduleRandomReboot();
            this.rebootScheduled = true;
        }
    }
};

// Add IP Assignment Watchdog module
let IpAssignmentWatchdog = {
    failCounter: 0,
    pingTimer: null,
    
    // Helper function for logging
    log: function(message) {
        if (CONFIG.debug) {
            print("[IpAssignmentWatchdog] " + message);
        }
    },
    
    // Check if the device has a valid IP assignment
    checkForIp: function() {
        // Check WiFi connection
        const wifi = Shelly.getComponentStatus('wifi');
        const isWifiConnected = wifi.status === 'got ip';
        
        // Check Ethernet connection
        const eth = Shelly.getComponentStatus('eth');
        const isEthConnected = eth.status === 'got ip';
        
        // Connection is now established OR was never broken
        // Reset counter and start over
        if (isWifiConnected || isEthConnected) {
            this.log("WiFi or Ethernet works correctly. Resetting counter to 0");
            this.failCounter = 0;
            return;
        }
        
        // If not connected, increment counter of failures
        this.failCounter++;
        
        if (this.failCounter < CONFIG.ipAssignment.numberOfFails) {
            const remainingAttemptsBeforeRestart = CONFIG.ipAssignment.numberOfFails - this.failCounter;
            this.log("WiFi or Ethernet healthcheck failed " + this.failCounter + " out of " + 
                    CONFIG.ipAssignment.numberOfFails + " times");
            return;
        }
        
        // Check if reboot is locked
        if (SHARED_STATE.rebootLock) {
            this.log("Reboot prevented: " + SHARED_STATE.rebootLockReason);
            return;
        }
        
        this.log("WiFi or Ethernet healthcheck failed all attempts. Restarting device...");
        Shelly.call('Shelly.Reboot');
    },
    
    // Setup status handler for switch events
    setupStatusHandler: function() {
        let self = this;
        Shelly.addStatusHandler(function(status) {
            // Is the component a switch
            if (status.name !== "switch") return;
            
            // Is it the one with id 0
            if (status.id !== 0) return;
            
            // Does it have a delta.source property
            if (typeof status.delta.source === "undefined") return;
            
            // Is the source a timer
            if (status.delta.source !== "timer") return;
            
            // Is it turned on
            if (status.delta.output !== true) return;
            
            Timer.clear(self.pingTimer);
            
            // Start the loop to ping the endpoints again
            self.pingTimer = Timer.set(CONFIG.ipAssignment.retryIntervalSeconds * 1000, true, function() {
                self.checkForIp();
            });
        });
    },
    
    // Initialize the IP assignment watchdog
    init: function() {
        this.log("Starting IP monitor");
        let self = this;
        this.pingTimer = Timer.set(CONFIG.ipAssignment.retryIntervalSeconds * 1000, true, function() {
            self.checkForIp();
        });
        this.setupStatusHandler();
    }
};

// Add Firmware Update module
let FirmwareUpdater = {
    updateTimer: null,
    lastCheckTimestamp: 0,
    
    // Helper function for logging
    log: function(message) {
        if (CONFIG.debug) {
            print("[FirmwareUpdater] " + message);
        }
    },
    
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
            let updateInfo = null;
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
        const checkIntervalMs = CONFIG.firmwareUpdate.checkIntervalDays * 24 * 60 * 60 * 1000;
        
        // Clear any existing timer
        if (this.updateTimer !== null) {
            Timer.clear(this.updateTimer);
        }
        
        // Schedule the next check
        let self = this;
        this.updateTimer = Timer.set(checkIntervalMs, false, function() {
            self.checkForUpdate();
            self.scheduleNextCheck(); // Schedule the next check after this one completes
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

// Remote Logger implementation
let RemoteLogger = {
    deviceId: null,
    deviceName: null,
    
    // Log severity levels
    severity: {
        EMERGENCY: "emergency",
        ALERT: "alert",
        CRITICAL: "critical",
        ERROR: "error",
        WARNING: "warning",
        NOTICE: "notice",
        INFO: "info",
        DEBUG: "debug"
    },
    
    // Initialize the logger
    init: function() {
        if (!CONFIG.logging.enabled) {
            print("[RemoteLogger] Remote logging is disabled in configuration");
            return;
        }
        
        try {
            // Get device ID from Shelly.getDeviceInfo()
            const deviceInfo = Shelly.getDeviceInfo();
            if (!deviceInfo || !deviceInfo.id) {
                print("[RemoteLogger] Failed to get device ID");
                return;
            }
            
            this.deviceId = deviceInfo.id;
            
            // Get device name from Sys.GetConfig
            const sysConfig = Shelly.getComponentConfig("sys");
            if (sysConfig && sysConfig.device && sysConfig.device.name) {
                this.deviceName = sysConfig.device.name;
            } else {
                // Fallback to device ID if name is not set
                this.deviceName = this.deviceId;
            }
            
            SHARED_STATE.syslogEnabled = true;
            print("[RemoteLogger] Logger initialized for device: " + this.deviceName);
        } catch (e) {
            print("[RemoteLogger] Error during initialization: " + e.message);
        }
    },
    
    // Format a log message as JSON
    formatMessage: function(severity, message) {
        // Get timestamp in ISO format
        const now = new Date();
        const timestamp = now.toISOString();
        
        // Create a log object
        return JSON.stringify({
            timestamp: timestamp,
            severity: severity,
            device_id: this.deviceId,
            device_name: this.deviceName,
            app: CONFIG.logging.appName,
            message: message
        });
    },
    
    // Send a message with specified severity
    send: function(severity, message) {
        if (!SHARED_STATE.syslogEnabled) {
            return false;
        }
        
        try {
            const logMessage = this.formatMessage(severity, message);
            
            if (CONFIG.logging.method === "webhook") {
                // Send log via HTTP POST
                Shelly.call("HTTP.POST", {
                    url: CONFIG.logging.url,
                    body: logMessage,
                    content_type: "application/json"
                }, function(result, error_code, error_message) {
                    if (error_code) {
                        print("[RemoteLogger] Error sending HTTP log: " + error_message);
                    }
                });
            } else if (CONFIG.logging.method === "mqtt" && MQTT.isConnected()) {
                // Send log via MQTT if connected
                MQTT.publish(CONFIG.logging.mqttTopic, logMessage, 0, false);
            }
            
            return true;
        } catch (e) {
            print("[RemoteLogger] Error sending message: " + e.message);
            return false;
        }
    },
    
    // Convenience methods for different severity levels
    emergency: function(message) { return this.send(this.severity.EMERGENCY, message); },
    alert: function(message) { return this.send(this.severity.ALERT, message); },
    critical: function(message) { return this.send(this.severity.CRITICAL, message); },
    error: function(message) { return this.send(this.severity.ERROR, message); },
    warning: function(message) { return this.send(this.severity.WARNING, message); },
    notice: function(message) { return this.send(this.severity.NOTICE, message); },
    info: function(message) { return this.send(this.severity.INFO, message); },
    debug: function(message) { return this.send(this.severity.DEBUG, message); }
};

// Add Prometheus Metrics module
let PrometheusMetrics = {
    // Constants
    TYPE_GAUGE: "gauge",
    TYPE_COUNTER: "counter",
    
    // Device info
    deviceInfo: null,
    defaultLabels: [],
    
    // Helper function for logging
    log: function(message) {
        if (CONFIG.debug) {
            print("[PrometheusMetrics] " + message);
        }
    },
    
    // Initialize Prometheus metrics
    init: function() {
        if (!CONFIG.prometheus || !CONFIG.prometheus.enabled) {
            this.log("Prometheus metrics are disabled in configuration");
            return;
        }
        
        try {
            // Get device info
            this.deviceInfo = Shelly.getDeviceInfo();
            
            // Set up default labels
            this.defaultLabels = [
                ["name", this.deviceInfo.name],
                ["id", this.deviceInfo.id],
                ["mac", this.deviceInfo.mac],
                ["app", this.deviceInfo.app]
            ].map(function(data) {
                return this.promLabel(data[0], data[1]);
            }, this);
            
            // Register HTTP endpoint
            const endpoint = CONFIG.prometheus.endpoint || "metrics";
            HTTPServer.registerEndpoint(endpoint, function(request, response) {
                PrometheusMetrics.httpServerHandler(request, response);
            });
            
            this.log("Prometheus metrics endpoint registered at /" + endpoint);
        } catch (e) {
            this.log("Error initializing Prometheus metrics: " + e.message);
        }
    },
    
    // Create a Prometheus label
    promLabel: function(label, value) {
        return [label, "=", '"', value, '"'].join("");
    },
    
    // Generate one metric output
    printPrometheusMetric: function(name, type, specificLabels, description, value) {
        const metricPrefix = "shelly_";
        return [
            "# HELP ", metricPrefix, name, " ", description, "\n",
            "# TYPE ", metricPrefix, name, " ", type, "\n",
            metricPrefix, name, "{", this.defaultLabels.join(","), specificLabels.length > 0 ? "," : "", specificLabels.join(","), "}", " ", value, "\n\n"
        ].join("");
    },
    
    // HTTP handler for metrics endpoint
    httpServerHandler: function(request, response) {
        response.body = [
            this.generateMetricsForSystem(),
            this.generateMetricsForSwitches()
        ].join("");
        response.code = 200;
        response.headers = [["Content-Type", "text/plain; version=0.0.4"]];
        response.send();
    },
    
    // Generate metrics for the system
    generateMetricsForSystem: function() {
        const sys = Shelly.getComponentStatus("sys");
        return [
            this.printPrometheusMetric("uptime_seconds", this.TYPE_COUNTER, [], "System uptime in seconds", sys.uptime),
            this.printPrometheusMetric("ram_size_bytes", this.TYPE_GAUGE, [], "Internal board RAM size in bytes", sys.ram_size),
            this.printPrometheusMetric("ram_free_bytes", this.TYPE_GAUGE, [], "Internal board free RAM size in bytes", sys.ram_free),
            // Add MQTT watchdog metrics
            this.printPrometheusMetric("mqtt_fail_counter", this.TYPE_GAUGE, [], "MQTT connection failure counter", MqttWatchdog.failCounter)
        ].join("");
    },
    
    // Generate metrics for all monitored switches
    generateMetricsForSwitches: function() {
        const monitoredSwitches = CONFIG.prometheus.monitoredSwitches || ["switch:0"];
        
        let result = "";
        for (let i = 0; i < monitoredSwitches.length; i++) {
            result += this.generateMetricsForSwitch(monitoredSwitches[i]);
        }
        
        return result;
    },
    
    // Generate metrics for a specific switch
    generateMetricsForSwitch: function(stringId) {
        try {
            const sw = Shelly.getComponentStatus(stringId);
            if (!sw) {
                this.log("Switch not found: " + stringId);
                return "";
            }
            
            const switchLabel = this.promLabel("switch", sw.id);
            
            return [
                this.printPrometheusMetric("switch_power_watts", this.TYPE_GAUGE, [switchLabel], "Instant power consumption in watts", sw.apower || 0),
                this.printPrometheusMetric("switch_voltage_volts", this.TYPE_GAUGE, [switchLabel], "Instant voltage in volts", sw.voltage || 0),
                this.printPrometheusMetric("switch_current_amperes", this.TYPE_GAUGE, [switchLabel], "Instant current in amperes", sw.current || 0),
                this.printPrometheusMetric("switch_temperature_celsius", this.TYPE_GAUGE, [switchLabel], "Temperature of the device in celsius", sw.temperature && sw.temperature.tC ? sw.temperature.tC : 0),
                this.printPrometheusMetric("switch_power_total", this.TYPE_COUNTER, [switchLabel], "Accumulated energy consumed in watts hours", sw.aenergy && sw.aenergy.total ? sw.aenergy.total : 0),
                this.printPrometheusMetric("switch_output", this.TYPE_GAUGE, [switchLabel], "Switch state (1=on, 0=off)", sw.output ? 1 : 0)
            ].join("");
        } catch (e) {
            this.log("Error generating metrics for switch " + stringId + ": " + e.message);
            return "";
        }
    }
};

// Initialize all components
MqttWatchdog.init();
DailyReboot.init();
IpAssignmentWatchdog.init();
FirmwareUpdater.init();
RemoteLogger.init();
PrometheusMetrics.init();
