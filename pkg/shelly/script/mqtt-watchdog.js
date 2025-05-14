// mqtt-watchdog.js
let CONFIG = {
    numberOfFails: 5,
    retryIntervalSeconds: 10,
    notificationTopic: "notifications/mqtt-watchdog",
    debugLog: true
}

let failCounter = 0;
let timer = null;

// Helper function for logging
function log(message) {
    if (CONFIG.debugLog) {
        print(message);
    }
}

// Attempt to reconnect MQTT by rebooting the device
function attemptReconnect() {
    log("Attempting to reconnect MQTT by rebooting device");
    Shelly.call("Shelly.Reboot", {});
}

// Check if MQTT is enabled in configuration
function isMqttEnabled() {
    let mqttConfig = Shelly.getComponentConfig("mqtt");
    return mqttConfig && mqttConfig.enable === true;
}

// Check if MQTT is connected
function isMqttConnected() {
    return MQTT.isConnected();
}

// Regular check function that runs on a timer
function checkMqttConnection() {
    if (!isMqttEnabled()) {
        log("MQTT is not enabled in configuration");
        return;
    }
    
    if (isMqttConnected()) {
        if (failCounter > 0) {
            log("MQTT connection restored after " + failCounter + " failures");
        }
        failCounter = 0;
    } else {
        failCounter++;
        log("MQTT connection check failed: " + failCounter + "/" + CONFIG.numberOfFails);
        
        if (failCounter >= CONFIG.numberOfFails) {
            log("Reached maximum failures, rebooting device");
            attemptReconnect();
            return; // Don't schedule another check since we're rebooting
        }
    }
    
    // Schedule the next check
    timer = Timer.set(CONFIG.retryIntervalSeconds * 1000, false, checkMqttConnection);
}

// Start the watchdog if MQTT is enabled
if (isMqttEnabled()) {
    log("Starting MQTT watchdog");
    checkMqttConnection();
} else {
    log("MQTT is not enabled, skipping watchdog");
}
