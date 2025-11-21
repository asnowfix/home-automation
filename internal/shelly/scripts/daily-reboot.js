// daily-reboot.js - Daily Reboot Scheduler for Shelly devices
// Schedules a random reboot once per day within a configured time window
// Helps maintain device stability and clear any accumulated state

// Configuration
var CONFIG = {
    windowStartHour: 2,     // Earliest hour to reboot (2 = 2:00 AM)
    windowEndHour: 5,       // Latest hour to reboot (5 = 5:59 AM)
    debug: true             // Enable debug logging to console
};

// Shared state
var STATE = {
    rebootLock: false,      // When true, prevents reboots
    rebootLockReason: ""    // Reason for the reboot lock
};

// Daily Reboot module
var DailyReboot = {
    rebootScheduled: false,
    
    // Helper function for logging
    log: function(message) {
        if (CONFIG.debug) {
            print("[DailyReboot] " + message);
        }
    },
    
    // Generate random integer between min (inclusive) and max (exclusive)
    getRandomInt: function(min, max) {
        return Math.floor(Math.random() * (max - min)) + min;
    },
    
    // Schedule a random reboot within the configured time window
    scheduleRandomReboot: function() {
        // Get current date/time
        var now = new Date();
        var tomorrow = new Date(now.getTime() + 24*60*60*1000);
        
        // Pick a random hour/minute in the window
        var hour = this.getRandomInt(CONFIG.windowStartHour, CONFIG.windowEndHour + 1);
        var minute = this.getRandomInt(0, 60);
        
        // Create target time for tomorrow
        var target = new Date(tomorrow.getFullYear(), tomorrow.getMonth(), tomorrow.getDate(), hour, minute, 0, 0);
        var delayMs = target.getTime() - now.getTime();
        
        this.log("Scheduling next reboot at " + target.toISOString() + " (in " + Math.round(delayMs / 3600000) + " hours)");
        
        var self = this;
        Timer.set(delayMs, false, function() {
            // Check if reboot is locked
            if (STATE.rebootLock) {
                self.log("Scheduled reboot prevented: " + STATE.rebootLockReason);
                // Reschedule for tomorrow
                self.scheduleRandomReboot();
                return;
            }
            
            self.log("Rebooting device now...");
            Shelly.call("Sys.Reboot", null, null);
            
            // After reboot, this script will restart and reschedule
            // But we call it here too in case the reboot is delayed
            self.scheduleRandomReboot();
        });
    },
    
    // Initialize the daily reboot scheduler
    init: function() {
        if (!this.rebootScheduled) {
            this.log("Initializing daily reboot scheduler (window: " + 
                    CONFIG.windowStartHour + ":00 - " + CONFIG.windowEndHour + ":59)");
            this.scheduleRandomReboot();
            this.rebootScheduled = true;
        }
    }
};

// Initialize the scheduler
(function() {
    print("Script starting...");
    
    DailyReboot.init();
    
    print("Script initialization complete");
    
    // Add stop event handler
    Shelly.addEventHandler(function(eventData) {
        if (eventData && eventData.info && eventData.info.event === "script_stop") {
            print("Script stopping");
        }
    });
})();
