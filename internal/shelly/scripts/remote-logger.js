// remote-logger.js - Remote logging for Shelly devices
// Sends device logs to remote endpoints via HTTP webhook or MQTT
// Supports RFC 5424 syslog severity levels

// Configuration
var CONFIG = {
    enabled: true,            // Set to false to disable remote logging
    method: "mqtt",           // "webhook" or "mqtt"
    url: "http://192.168.1.100:8080/logs", // Webhook URL for HTTP logging
    mqttTopic: "shelly/logs", // MQTT topic for logging if method is "mqtt"
    hostname: "shelly",       // Device hostname in logs (will be overridden by actual device name)
    appName: "remote-logger", // Application name in logs
    debug: true               // Enable debug logging to console
};

// Remote Logger implementation
var RemoteLogger = {
    deviceId: null,
    deviceName: null,
    
    // RFC 5424 Syslog severity levels
    severity: {
        EMERGENCY: 0,  // System is unusable
        ALERT: 1,      // Action must be taken immediately
        CRITICAL: 2,   // Critical conditions
        ERROR: 3,      // Error conditions
        WARNING: 4,    // Warning conditions
        NOTICE: 5,     // Normal but significant condition
        INFO: 6,       // Informational messages
        DEBUG: 7       // Debug-level messages
    },
    
    // Initialize the logger
    init: function() {
        if (!CONFIG.enabled) {
            print("[RemoteLogger] Remote logging is disabled in configuration");
            return;
        }
        
        try {
            // Get device ID from Shelly.getDeviceInfo()
            var deviceInfo = Shelly.getDeviceInfo();
            if (!deviceInfo || !deviceInfo.id) {
                print("[RemoteLogger] Failed to get device ID");
                return;
            }
            
            this.deviceId = deviceInfo.id;
            this.deviceName = deviceInfo.name || CONFIG.hostname;
            
            // Override hostname with actual device name
            if (deviceInfo.name) {
                CONFIG.hostname = deviceInfo.name;
            }
            
            print("[RemoteLogger] Logger initialized for device: " + this.deviceName);
        } catch (e) {
            print("[RemoteLogger] Error during initialization: " + e.message);
        }
    },
    
    // Format a log message as JSON
    formatMessage: function(severity, message) {
        // Get timestamp in ISO format
        var now = new Date();
        var timestamp = now.toISOString();
        
        // Create a log object
        return JSON.stringify({
            timestamp: timestamp,
            hostname: CONFIG.hostname,
            app: CONFIG.appName,
            severity: severity,
            message: message,
            device_id: this.deviceId
        });
    },
    
    // Send a log message
    send: function(severity, message) {
        if (!CONFIG.enabled) {
            return false;
        }
        
        try {
            var logMessage = this.formatMessage(severity, message);
            
            if (CONFIG.method === "webhook") {
                // Send log via HTTP POST
                Shelly.call("HTTP.POST", {
                    url: CONFIG.url,
                    body: logMessage,
                    timeout: 5,
                    ssl_ca: "*",
                    content_type: "application/json"
                }, function(result, error_code, error_message) {
                    if (error_code) {
                        print("[RemoteLogger] Error sending HTTP log: " + error_message);
                    }
                });
            } else if (CONFIG.method === "mqtt" && MQTT.isConnected()) {
                // Send log via MQTT
                MQTT.publish(CONFIG.mqttTopic, logMessage, 0, false);
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

// Initialize the logger
(function() {
    print("Script starting...");
    
    RemoteLogger.init();
    
    print("Script initialization complete");
    
    // Add stop event handler
    Shelly.addEventHandler(function(eventData) {
        if (eventData && eventData.info && eventData.info.event === "script_stop") {
            print("Script stopping");
        }
    });
})();

// Example usage (commented out):
// RemoteLogger.info("Device started successfully");
// RemoteLogger.warning("Temperature sensor not responding");
// RemoteLogger.error("Failed to connect to MQTT broker");
