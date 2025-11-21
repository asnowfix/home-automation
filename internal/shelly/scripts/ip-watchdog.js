// ip-watchdog.js - IP Assignment Watchdog for Shelly devices
// Monitors network connectivity and reboots if no IP is assigned
// Checks both WiFi and Ethernet connections

// Configuration
var CONFIG = {
    numberOfFails: 5,           // Number of failures before triggering a restart
    retryIntervalSeconds: 60,   // Time in seconds between retries
    debug: true,                // Enable debug logging to console
    monitorSwitch: true,        // Monitor switch:0 timer events for additional checks
    switchId: 0                 // Which switch to monitor (0 = switch:0)
};

// Shared state
var STATE = {
    rebootLock: false,          // When true, prevents reboots
    rebootLockReason: ""        // Reason for the reboot lock
};

// IP Assignment Watchdog module
var IpAssignmentWatchdog = {
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
        var wifi = null;
        try { 
            wifi = Shelly.getComponentStatus('wifi'); 
        } catch (e) { 
            wifi = null; 
            if (CONFIG.debug) {
                this.log("WiFi status error: " + (e && e.message ? e.message : ""));
            }
        }
        var isWifiConnected = (wifi && wifi.status === 'got ip');
        
        // Check Ethernet connection
        var eth = null;
        try { 
            eth = Shelly.getComponentStatus('eth'); 
        } catch (e) { 
            eth = null; 
            if (CONFIG.debug) {
                this.log("Ethernet status error: " + (e && e.message ? e.message : ""));
            }
        }
        var isEthConnected = (eth && eth.status === 'got ip');
        
        // Connection is now established OR was never broken
        // Reset counter and start over
        if (isWifiConnected || isEthConnected) {
            if (this.failCounter > 0) {
                this.log("WiFi or Ethernet works correctly. Resetting counter to 0");
            }
            this.failCounter = 0;
            return;
        }
        
        // If not connected, increment counter of failures
        this.failCounter++;
        
        if (this.failCounter < CONFIG.numberOfFails) {
            var remainingAttemptsBeforeRestart = CONFIG.numberOfFails - this.failCounter;
            this.log("WiFi or Ethernet healthcheck failed " + this.failCounter + " out of " + 
                    CONFIG.numberOfFails + " times (remaining: " + remainingAttemptsBeforeRestart + ")");
            return;
        }
        
        // Check if reboot is locked
        if (STATE.rebootLock) {
            this.log("Reboot prevented: " + STATE.rebootLockReason);
            return;
        }
        
        this.log("WiFi or Ethernet healthcheck failed all attempts. Restarting device...");
        Shelly.call('Shelly.Reboot');
    },
    
    // Setup status handler for switch events
    setupStatusHandler: function() {
        if (!CONFIG.monitorSwitch) {
            return;
        }
        
        var self = this;
        Shelly.addStatusHandler(function(status) {
            // Is the component a switch
            if (status.name !== "switch") return;
            
            // Is it the configured switch
            if (status.id !== CONFIG.switchId) return;
            
            // Does it have a delta.source property
            if (!status.delta || typeof status.delta.source === "undefined") return;
            
            // Is the source a timer
            if (status.delta.source !== "timer") return;
            
            // Is it turned on
            if (!status.delta || status.delta.output !== true) return;
            
            // Trigger immediate check when switch is activated by timer
            // The recurring timer is already running from init()
            self.checkForIp();
        });
    },
    
    // Initialize the IP assignment watchdog
    init: function() {
        this.log("Starting IP monitor (check interval: " + CONFIG.retryIntervalSeconds + "s)");
        var self = this;
        this.pingTimer = Timer.set(CONFIG.retryIntervalSeconds * 1000, true, function() {
            self.checkForIp();
        });
        this.setupStatusHandler();
    }
};

// Initialize the watchdog
(function() {
    print("Script starting...");
    
    IpAssignmentWatchdog.init();
    
    print("Script initialization complete");
    
    // Add stop event handler
    Shelly.addEventHandler(function(eventData) {
        if (eventData && eventData.info && eventData.info.event === "script_stop") {
            print("Script stopping");
        }
    });
})();
